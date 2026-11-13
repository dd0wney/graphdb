// Package vector — E-means clustering (Bessa et al., 2026, arXiv:2611.09501).
//
// Implements Algorithm 1 of "e-means: outperforming k-means (in one-tenth the
// epochs)" from facebookresearch/faiss/contrib/e_means, including:
//   - Fitted batch-size and step-size scaling laws (Equations 6 & 7)
//   - Cosine step-size annealing with 0.1 floor (Equation 5)
//   - Mass-and-position exponential moving average (Equation 2)
//   - Dead-cluster revival via Gumbel-max proportional donor splitting
//   - Spherical unit-sphere projection with infinity-norm prescaling
//   - Final-epoch unweighted closing mean (finalize)
package vector

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

const (
	emeansAutoQualityMaxD = 16
	emeansNoRevivalMaxD   = 2
	emeansUnusedThreshold = 0.1
	emeansCheckEvery      = 5
	emeansSplitEpsilon    = float32(1.0 / 1024.0)
	emeansInitUsage       = float32(1.0 / 65536.0) // 2^-16
	emeansAlphaMinFactor  = 0.1
)

// Fitted scaling laws from faiss/contrib/e_means/base.py:
// B = C_B * k^0.134749 * n^0.876473 * T^0.813344
var emeansBatchLaws = map[string][4]float64{
	"throughput": {0.035134, 0.134749, 0.876473, 0.813344},
	"quality":    {0.00439175, 0.134749, 0.876473, 0.813344},
}

// alpha0 = 0.8244 * k^-0.4617 * B^0.9003 * n^-0.4498 * T^-0.1057
var emeansAlphaLaw = [5]float64{0.8244, -0.4617, 0.9003, -0.4498, -0.1057}

// EmeansConfig configures an E-means clustering run.
type EmeansConfig struct {
	D               int
	K               int
	NEpochs         float64 // default 5.0; fractional passes supported
	BatchLaw        string  // "auto" (default), "throughput", or "quality"
	BatchSize       int     // optional override; if 0, derived from BatchLaw
	Alpha0          float64 // optional override; if 0, derived from step law
	UnusedThreshold *float64
	Spherical       bool
	Seed            int64
}

// EmeansRecipe records the resolved hyperparameters and pass schedule.
type EmeansRecipe struct {
	BatchLaw       string
	BatchLawReason string
	BatchSize      int
	Alpha0         float64
	StepsPerPass   int
	TotalSteps     int
	EpochRows      []int
	Spherical      bool
}

// EmeansEpochStat records per-pass telemetry.
type EmeansEpochStat struct {
	Epoch           int
	Objective       float64
	ImbalanceFactor float64
	NSplit          int
}

// Emeans holds the state and results of an E-means clustering model.
type Emeans struct {
	Cfg            EmeansConfig
	Recipe         EmeansRecipe
	Centroids      [][]float32
	Assignments    []int
	IterationStats []EmeansEpochStat
	NSplits        int

	usage         []float32
	rawCentroids  []float32 // flat k * d
	unitCentroids []float32 // flat k * d
}

// NewEmeans validates configuration and returns a new Emeans instance.
func NewEmeans(cfg EmeansConfig) (*Emeans, error) {
	if cfg.D < 1 {
		return nil, fmt.Errorf("emeans: dimension D must be >= 1, got %d", cfg.D)
	}
	if cfg.K < 1 {
		return nil, fmt.Errorf("emeans: cluster count K must be >= 1, got %d", cfg.K)
	}
	if cfg.NEpochs == 0 {
		cfg.NEpochs = 5.0
	}
	if cfg.NEpochs < 0 || math.IsNaN(cfg.NEpochs) || math.IsInf(cfg.NEpochs, 0) {
		return nil, fmt.Errorf("emeans: NEpochs must be positive finite, got %v", cfg.NEpochs)
	}
	if cfg.BatchLaw == "" {
		cfg.BatchLaw = "auto"
	}
	if cfg.Seed == 0 {
		cfg.Seed = 1234
	}
	return &Emeans{Cfg: cfg}, nil
}

func roundPowerOfTwo(v float64) int {
	if v <= 1.0 {
		return 1
	}
	exp := math.Floor(math.Log2(v))
	low := math.Pow(2, exp)
	high := 2.0 * low
	if v/low < high/v {
		return int(low)
	}
	return int(high)
}

func stepLaw(n, k int, nepochs float64, batchSize int) float64 {
	c, kExp, bExp, nExp, tExp := emeansAlphaLaw[0], emeansAlphaLaw[1], emeansAlphaLaw[2], emeansAlphaLaw[3], emeansAlphaLaw[4]
	a0 := c *
		math.Pow(float64(k), kExp) *
		math.Pow(float64(batchSize), bExp) *
		math.Pow(float64(n), nExp) *
		math.Pow(nepochs, tExp)
	if a0 < 1e-4 {
		return 1e-4
	}
	if a0 > 1.0 {
		return 1.0
	}
	return a0
}

func scheduledAlpha(alpha0 float64, step, totalSteps int) float32 {
	if totalSteps <= 1 {
		return float32(alpha0)
	}
	progress := float64(step) / float64(totalSteps)
	if progress > 1.0 {
		progress = 1.0
	}
	cosFactor := 0.5 * (1.0 + math.Cos(math.Pi*progress))
	factor := emeansAlphaMinFactor + (1.0-emeansAlphaMinFactor)*cosFactor
	return float32(alpha0 * factor)
}

func resolveRecipe(n int, cfg EmeansConfig) (EmeansRecipe, error) {
	law := cfg.BatchLaw
	reason := "requested"
	if law == "auto" {
		if cfg.D <= emeansAutoQualityMaxD {
			law = "quality"
			reason = fmt.Sprintf("d <= %d", emeansAutoQualityMaxD)
		} else {
			law = "throughput"
			reason = fmt.Sprintf("d > %d", emeansAutoQualityMaxD)
		}
	}
	coeffs, ok := emeansBatchLaws[law]
	if !ok {
		return EmeansRecipe{}, fmt.Errorf("emeans: unknown batch_law %q", cfg.BatchLaw)
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		rawB := coeffs[0] *
			math.Pow(float64(cfg.K), coeffs[1]) *
			math.Pow(float64(n), coeffs[2]) *
			math.Pow(cfg.NEpochs, coeffs[3])
		batchSize = roundPowerOfTwo(rawB)
		// For small subgraphs, ensure mini-batch has enough points while staying <= n/2.
		minFloor := 16
		if cfg.K > minFloor {
			minFloor = roundPowerOfTwo(float64(cfg.K))
		}
		if batchSize < minFloor {
			batchSize = minFloor
		}
		maxCap := n / 2
		if maxCap < 1 {
			maxCap = 1
		}
		if batchSize > maxCap {
			batchSize = roundPowerOfTwo(float64(maxCap))
			if batchSize > maxCap && batchSize > 1 {
				batchSize /= 2
			}
		}
		if batchSize < 1 {
			batchSize = 1
		}
	}

	alpha0 := cfg.Alpha0
	if alpha0 <= 0 {
		alpha0 = stepLaw(n, cfg.K, cfg.NEpochs, batchSize)
	}

	fullPasses := int(math.Floor(cfg.NEpochs))
	remFrac := cfg.NEpochs - float64(fullPasses)
	remRows := int(math.Round(remFrac * float64(n)))
	var epochRows []int
	for i := 0; i < fullPasses; i++ {
		epochRows = append(epochRows, n)
	}
	if remRows > 0 || len(epochRows) == 0 {
		if remRows <= 0 {
			remRows = n
		}
		epochRows = append(epochRows, remRows)
	}

	stepsPerPass := (n + batchSize - 1) / batchSize
	totalSteps := 0
	for _, r := range epochRows {
		totalSteps += (r + batchSize - 1) / batchSize
	}

	return EmeansRecipe{
		BatchLaw:       law,
		BatchLawReason: reason,
		BatchSize:      batchSize,
		Alpha0:         alpha0,
		StepsPerPass:   stepsPerPass,
		TotalSteps:     totalSteps,
		EpochRows:      epochRows,
		Spherical:      cfg.Spherical,
	}, nil
}

// normalizeRowInto projects a d-dimensional slice onto the unit sphere with
// infinity-norm prescaling (matching faiss/contrib/e_means/e_means_spherical.py).
// Zero vectors map to zero.
func normalizeRowInto(src, dst []float32) {
	maxAbs := float32(0)
	for _, v := range src {
		av := float32(math.Abs(float64(v)))
		if av > maxAbs {
			maxAbs = av
		}
	}
	if maxAbs == 0 {
		copy(dst, src)
		return
	}
	var sumSq float64
	for i, v := range src {
		scaled := v / maxAbs
		dst[i] = scaled
		sumSq += float64(scaled) * float64(scaled)
	}
	norm := float32(math.Sqrt(sumSq))
	if norm == 0 {
		return
	}
	for i := range dst {
		dst[i] /= norm
	}
}

func (em *Emeans) refreshReadout() []float32 {
	if !em.Cfg.Spherical {
		return em.rawCentroids
	}
	d := em.Cfg.D
	for j := 0; j < em.Cfg.K; j++ {
		off := j * d
		normalizeRowInto(em.rawCentroids[off:off+d], em.unitCentroids[off:off+d])
	}
	return em.unitCentroids
}

func (em *Emeans) assignRow(row, readout []float32) (int, float64) {
	d := em.Cfg.D
	k := em.Cfg.K
	if em.Cfg.Spherical {
		bestJ := 0
		bestSim := float32(-math.MaxFloat32)
		for j := 0; j < k; j++ {
			off := j * d
			var dot float32
			c := readout[off : off+d]
			for i := 0; i < d; i++ {
				dot += row[i] * c[i]
			}
			if dot > bestSim {
				bestSim = dot
				bestJ = j
			}
		}
		// Spherical objective contribution: 2 - 2 * dot
		return bestJ, 2.0 - 2.0*float64(bestSim)
	}
	bestJ := 0
	bestDist := float32(math.MaxFloat32)
	for j := 0; j < k; j++ {
		off := j * d
		var dist float32
		c := readout[off : off+d]
		for i := 0; i < d; i++ {
			diff := row[i] - c[i]
			dist += diff * diff
		}
		if dist < bestDist {
			bestDist = dist
			bestJ = j
		}
	}
	return bestJ, float64(bestDist)
}

func (em *Emeans) reviveDeadClusters(rng *rand.Rand, tau float64) int {
	if tau <= 0 {
		return 0
	}
	k := em.Cfg.K
	d := em.Cfg.D
	var totalUsage float64
	for _, u := range em.usage {
		totalUsage += float64(u)
	}
	thresh := float32(totalUsage * tau / float64(k))

	var unused []int
	type donorKey struct {
		idx int
		key float64
	}
	var donors []donorKey
	for j := 0; j < k; j++ {
		if em.usage[j] < thresh {
			unused = append(unused, j)
		} else if em.usage[j] > 0 {
			u := rng.Float64()
			if u < 1e-300 {
				u = 1e-300
			}
			// Gumbel-max sampling without replacement proportional to usage[j]
			key := math.Log(float64(em.usage[j])) - math.Log(-math.Log(u))
			donors = append(donors, donorKey{idx: j, key: key})
		}
	}
	nsplit := len(unused)
	if len(donors) < nsplit {
		nsplit = len(donors)
	}
	if nsplit == 0 {
		return 0
	}
	sort.Slice(donors, func(a, b int) bool {
		return donors[a].key > donors[b].key
	})

	for s := 0; s < nsplit; s++ {
		src := donors[s].idx
		dst := unused[s]
		half := em.usage[src] / 2.0
		em.usage[src] = half
		em.usage[dst] = half

		srcOff := src * d
		dstOff := dst * d
		allSame := true
		for i := 0; i < d; i++ {
			sign := float32(1.0)
			if i%2 == 1 {
				sign = -1.0
			}
			offset := emeansSplitEpsilon * sign
			cVal := em.rawCentroids[srcOff+i]
			donorVal := cVal * (1.0 - offset)
			childVal := cVal * (1.0 + offset)
			em.rawCentroids[srcOff+i] = donorVal
			em.rawCentroids[dstOff+i] = childVal
			if donorVal != childVal {
				allSame = false
			}
		}
		if allSame && d > 0 {
			em.rawCentroids[dstOff] = math.Nextafter32(em.rawCentroids[dstOff], float32(math.Inf(1)))
		}
	}
	em.NSplits += nsplit
	return nsplit
}

// Train runs E-means on the input vectors and returns the final (k, d) centroids
// and exact nearest-centroid assignment labels for each input vector.
func (em *Emeans) Train(vectors [][]float32) ([][]float32, []int, error) {
	return em.TrainContext(context.Background(), vectors)
}

// TrainContext is Train that stops when ctx ends. It returns an error wrapping
// ctx.Err() and no centroids. The check runs once per mini-batch, which is the
// loop that scales with the input and is large enough that the check is free.
//
// A failed or cancelled run leaves the model as it was before the call. The
// body replaces every slice it writes with a fresh allocation, so a shallow
// copy of the receiver taken first is a complete snapshot. Without the restore,
// Assign would read half-trained centroids while Centroids and Assignments
// still described the old model.
func (em *Emeans) TrainContext(ctx context.Context, vectors [][]float32) ([][]float32, []int, error) {
	// Before any work, and before the n <= k shortcut, which has no loop.
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	prior := *em
	centroids, labels, err := em.train(ctx, vectors)
	if err != nil {
		*em = prior
		return nil, nil, err
	}
	return centroids, labels, nil
}

func (em *Emeans) train(ctx context.Context, vectors [][]float32) ([][]float32, []int, error) {
	n := len(vectors)
	if n == 0 {
		return nil, nil, fmt.Errorf("emeans: cannot train on empty vector slice")
	}
	d := em.Cfg.D
	k := em.Cfg.K
	for i, v := range vectors {
		if len(v) != d {
			return nil, nil, fmt.Errorf("emeans: vector[%d] has length %d, want %d", i, len(v), d)
		}
	}

	recipe, err := resolveRecipe(n, em.Cfg)
	if err != nil {
		return nil, nil, err
	}
	em.Recipe = recipe

	tau := emeansUnusedThreshold
	if em.Cfg.D <= emeansNoRevivalMaxD {
		tau = 0.0
	}
	if em.Cfg.UnusedThreshold != nil {
		tau = *em.Cfg.UnusedThreshold
	}

	em.rawCentroids = make([]float32, k*d)
	em.unitCentroids = make([]float32, k*d)
	em.usage = make([]float32, k)
	for j := range em.usage {
		em.usage[j] = emeansInitUsage
	}

	rng := rand.New(rand.NewSource(em.Cfg.Seed))
	splitRng := rand.New(rand.NewSource(em.Cfg.Seed + 7919))

	// Initialize centroids from random distinct rows (or wrap if n < k).
	perm := rng.Perm(n)
	for j := 0; j < k; j++ {
		srcIdx := perm[j%n]
		off := j * d
		copy(em.rawCentroids[off:off+d], vectors[srcIdx])
		if em.Cfg.Spherical {
			normalizeRowInto(em.rawCentroids[off:off+d], em.rawCentroids[off:off+d])
		}
	}

	// Trivial fast path when n <= k: each point is its own centroid.
	if n <= k {
		em.Centroids = make([][]float32, k)
		for j := 0; j < k; j++ {
			c := make([]float32, d)
			off := j * d
			if em.Cfg.Spherical {
				normalizeRowInto(em.rawCentroids[off:off+d], c)
			} else {
				copy(c, em.rawCentroids[off:off+d])
			}
			em.Centroids[j] = c
		}
		labels := make([]int, n)
		for i := 0; i < n; i++ {
			labels[i] = i
		}
		em.Assignments = labels
		return em.Centroids, labels, nil
	}

	batchSums := make([]float32, k*d)
	batchCounts := make([]int, k)
	passCounts := make([]float64, k)
	passSums := make([]float64, k*d)

	step := 0
	numEpochs := len(recipe.EpochRows)
	em.IterationStats = make([]EmeansEpochStat, 0, numEpochs)

	for epIdx, epRows := range recipe.EpochRows {
		isLastEpoch := epIdx == numEpochs-1
		for j := range passCounts {
			passCounts[j] = 0
		}
		if isLastEpoch {
			for i := range passSums {
				passSums[i] = 0
			}
		}
		var epObj float64
		splitsBefore := em.NSplits
		epPerm := rng.Perm(n)

		for begin := 0; begin < epRows; begin += recipe.BatchSize {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			end := begin + recipe.BatchSize
			if end > epRows {
				end = epRows
			}
			for i := range batchSums {
				batchSums[i] = 0
			}
			for j := range batchCounts {
				batchCounts[j] = 0
			}

			readout := em.refreshReadout()
			var batchObj float64
			for idx := begin; idx < end; idx++ {
				row := vectors[epPerm[idx]]
				lbl, dist := em.assignRow(row, readout)
				batchCounts[lbl]++
				batchObj += dist
				off := lbl * d
				for i := 0; i < d; i++ {
					batchSums[off+i] += row[i]
				}
			}

			epObj += batchObj
			for j := 0; j < k; j++ {
				cnt := batchCounts[j]
				if cnt == 0 {
					continue
				}
				passCounts[j] += float64(cnt)
				if isLastEpoch {
					off := j * d
					for i := 0; i < d; i++ {
						passSums[off+i] += float64(batchSums[off+i])
					}
				}
			}

			alpha := scheduledAlpha(recipe.Alpha0, step, recipe.TotalSteps)
			decay := 1.0 - alpha
			for j := 0; j < k; j++ {
				em.usage[j] *= decay
			}
			for j := 0; j < k; j++ {
				cnt := batchCounts[j]
				if cnt == 0 {
					continue
				}
				cntF := float32(cnt)
				mass := em.usage[j] + alpha*cntF
				em.usage[j] = mass
				weight := (alpha * cntF) / mass
				off := j * d
				if weight >= 1.0 {
					for i := 0; i < d; i++ {
						em.rawCentroids[off+i] = batchSums[off+i] / cntF
					}
				} else {
					for i := 0; i < d; i++ {
						meanI := batchSums[off+i] / cntF
						em.rawCentroids[off+i] += weight * (meanI - em.rawCentroids[off+i])
					}
				}
			}

			step++
			if step%emeansCheckEvery == 0 && !isLastEpoch {
				em.reviveDeadClusters(splitRng, tau)
			}
		}

		var countSq float64
		for _, c := range passCounts {
			countSq += c * c
		}
		imb := float64(k) * countSq / (float64(epRows) * float64(epRows))
		em.IterationStats = append(em.IterationStats, EmeansEpochStat{
			Epoch:           epIdx + 1,
			Objective:       epObj / float64(epRows),
			ImbalanceFactor: imb,
			NSplit:          em.NSplits - splitsBefore,
		})
	}

	// Finalize: set each centroid to its unweighted last-epoch mean (Algorithm 1, line 12).
	em.Centroids = make([][]float32, k)
	for j := 0; j < k; j++ {
		off := j * d
		c := make([]float32, d)
		cnt := passCounts[j]
		if cnt > 0 {
			for i := 0; i < d; i++ {
				c[i] = float32(passSums[off+i] / cnt)
			}
		} else {
			copy(c, em.rawCentroids[off:off+d])
		}
		if em.Cfg.Spherical {
			normalizeRowInto(c, c)
			copy(em.unitCentroids[off:off+d], c)
		}
		copy(em.rawCentroids[off:off+d], c)
		em.Centroids[j] = c
	}

	_, labels, err := em.Assign(vectors)
	if err != nil {
		return nil, nil, err
	}
	em.Assignments = labels
	return em.Centroids, labels, nil
}

// Assign returns the distance (or cosine similarity when Spherical is true) and
// nearest centroid index for each input vector.
func (em *Emeans) Assign(vectors [][]float32) ([]float32, []int, error) {
	if len(em.Centroids) == 0 {
		return nil, nil, fmt.Errorf("emeans: model has not been trained")
	}
	d := em.Cfg.D
	readout := em.refreshReadout()
	scores := make([]float32, len(vectors))
	labels := make([]int, len(vectors))
	for idx, row := range vectors {
		if len(row) != d {
			return nil, nil, fmt.Errorf("emeans: vector[%d] has length %d, want %d", idx, len(row), d)
		}
		lbl, objDist := em.assignRow(row, readout)
		labels[idx] = lbl
		if em.Cfg.Spherical {
			// Convert (2 - 2*sim) back to cosine similarity in [-1, 1]
			scores[idx] = float32(1.0 - objDist/2.0)
		} else {
			scores[idx] = float32(objDist)
		}
	}
	return scores, labels, nil
}
