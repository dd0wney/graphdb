package storage

// The uniqueness-rules registry (ADR 0001, docs/adr/0001-uniqueness-rules-registry.md).
//
// graphdb hardcoded one coord-domain rule ("Claim" nodes are unique per
// for_task) at both write surfaces. This registry replaces that: a
// deployment declares (name, label, propertyKey) rules through the storage
// API, and CreateNodeWithUniquenessRulesForTenant (added in a later commit)
// is the one lookup path both surfaces call. graphdb itself ships no
// domain vocabulary.
//
// Persistence is a THIRD on-disk artefact alongside the two snapshot
// formats CLAUDE.md's "Snapshot format stability" section already covers —
// rules.json, versioned separately, in the same data directory. It is
// customer-data-adjacent (a stale or missing rule changes what writes a
// deployment accepts) so it follows the same durable-publish discipline as
// the JSON snapshot: write a temp file, fsync it, rename over the target,
// then fsync the parent directory (vfs_helpers.go's writeFileWithFS doc
// comment explains why both syncs are needed).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

const (
	// uniquenessRulesFileName is the on-disk artefact, alongside snapshot.json
	// / snapshot.mmap in the same data directory.
	uniquenessRulesFileName = "rules.json"

	// uniquenessRulesFileVersion is rules.json's own version field. It is
	// independent of both snapshot formats' version numbers (R3): changing
	// the registry's shape never bumps the snapshot version, and vice versa.
	uniquenessRulesFileVersion = 1

	// maxUniquenessRules caps how many rules a registry may hold, checked
	// both at load and on RegisterUniquenessRule (R6).
	maxUniquenessRules = 256

	// maxUniquenessRulesFileSize caps rules.json's size at load, checked
	// before the file is read into memory (R6).
	maxUniquenessRulesFileSize = 1 << 20 // 1 MiB

	// maxUniquenessRuleFieldLen bounds Name and PropertyKey.
	maxUniquenessRuleFieldLen = 100

	// maxUniquenessRuleLabelLen bounds Label.
	maxUniquenessRuleLabelLen = 50
)

// uniquenessNamePattern matches a valid Name or PropertyKey: it must start
// with a letter or underscore, then hold only letters, digits or
// underscores. Mirrors pkg/validation's unexported propKeyPattern.
//
// Not imported from pkg/validation: that package exposes the equivalent
// check only as ValidatePropertyKey, a single-purpose function with its own
// error wording, and has no exported single-label check at all (label
// validation there lives inside ValidateNodeRequest, which validates a
// whole node request, not one scalar field). Reusing it here would mean
// either accepting its error text for a field it does not know is a rule
// name, or constructing a throwaway NodeRequest to reach an unrelated
// path. A single local regexp for each shape is simpler than either.
var uniquenessNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// uniquenessLabelPattern matches a valid Label: one or more letters, digits
// or underscores. Mirrors pkg/validation's unexported labelPattern; see
// uniquenessNamePattern's comment for why it is not imported directly.
var uniquenessLabelPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// UniquenessRule declares that at most one node per tenant may carry the
// same value for (Label, PropertyKey). See ADR 0001.
type UniquenessRule struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	PropertyKey string `json:"propertyKey"`
}

// RequiredUniquenessRule names a rule a deployment insists must be
// registered before graphdb accepts a node write covered by Label.
//
// The pair, not a bare name, is what makes fail-closed a property of the
// DEPLOYMENT rather than of the label (ADR 0001): an unregistered rule name
// alone cannot tell graphdb which writes it would have covered, and the ADR
// explicitly rejects refusing every write when any required rule is
// missing (docs/adr/0001-uniqueness-rules-registry.md, "Where fail-closed
// lives").
type RequiredUniquenessRule struct {
	Name  string
	Label string
}

// uniquenessRulesDocument is rules.json's on-disk shape:
// {"version":1,"rules":[...]}.
type uniquenessRulesDocument struct {
	Version int              `json:"version"`
	Rules   []UniquenessRule `json:"rules"`
}

// uniquenessRulesPath returns the rules.json path for a data directory.
func uniquenessRulesPath(dataDir string) string {
	return filepath.Join(dataDir, uniquenessRulesFileName)
}

// validateUniquenessRule checks Name, Label and PropertyKey against the
// bounds and patterns ADR 0001's implementation notes specify. Called from
// both RegisterUniquenessRule and loadUniquenessRules, so the registry is
// safe from every caller — the file loader cannot admit a rule a live
// Register call would have refused.
func validateUniquenessRule(r UniquenessRule) error {
	if err := validateUniquenessNameOrKey("name", r.Name); err != nil {
		return err
	}
	if err := validateUniquenessLabel(r.Label); err != nil {
		return err
	}
	if err := validateUniquenessNameOrKey("propertyKey", r.PropertyKey); err != nil {
		return err
	}
	return nil
}

func validateUniquenessNameOrKey(field, value string) error {
	if value == "" {
		return fmt.Errorf("uniqueness rule %s must not be empty", field)
	}
	if len(value) > maxUniquenessRuleFieldLen {
		return fmt.Errorf("uniqueness rule %s %q exceeds %d bytes", field, value, maxUniquenessRuleFieldLen)
	}
	if !uniquenessNamePattern.MatchString(value) {
		return fmt.Errorf("uniqueness rule %s %q must match %s", field, value, uniquenessNamePattern.String())
	}
	return nil
}

func validateUniquenessLabel(label string) error {
	if label == "" {
		return fmt.Errorf("uniqueness rule label must not be empty")
	}
	if len(label) > maxUniquenessRuleLabelLen {
		return fmt.Errorf("uniqueness rule label %q exceeds %d bytes", label, maxUniquenessRuleLabelLen)
	}
	if !uniquenessLabelPattern.MatchString(label) {
		return fmt.Errorf("uniqueness rule label %q must match %s", label, uniquenessLabelPattern.String())
	}
	return nil
}

// loadUniquenessRules reads rules.json from the data directory into the
// registry. Called once from the constructor, after WAL init and before
// disk-backed-edge setup.
//
// A missing file is not an error: a fresh store, or one written before the
// registry existed, has an empty registry. A file that exists but cannot be
// fully trusted — too large, unparsable, an unknown version, too many
// rules, or one rule failing validation — refuses the WHOLE open, naming
// the file: rules.json is customer-data-adjacent (CLAUDE.md's
// format-stability rule), and a partially trusted registry is worse than no
// store, per the same reasoning ErrRecordUnreadable applies to a damaged
// snapshot record.
func (gs *GraphStorage) loadUniquenessRules() error {
	path := uniquenessRulesPath(gs.dataDir)

	info, err := gs.fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("uniqueness rules: stat %s: %w", path, err)
	}
	// Checked BEFORE the read, so an oversized file is never pulled into
	// memory just to be refused.
	if info.Size() > maxUniquenessRulesFileSize {
		return fmt.Errorf("uniqueness rules: %s is %d bytes, which exceeds the %d-byte limit",
			path, info.Size(), maxUniquenessRulesFileSize)
	}

	data, err := readFileWithFS(gs.fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			// Raced with a concurrent removal between Stat and Open; treat
			// the same as "never existed".
			return nil
		}
		return fmt.Errorf("uniqueness rules: read %s: %w", path, err)
	}

	var doc uniquenessRulesDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("uniqueness rules: %s does not parse as JSON: %w", path, err)
	}
	if doc.Version != uniquenessRulesFileVersion {
		return fmt.Errorf("uniqueness rules: %s has version %d, this build supports version %d",
			path, doc.Version, uniquenessRulesFileVersion)
	}
	if len(doc.Rules) > maxUniquenessRules {
		return fmt.Errorf("uniqueness rules: %s holds %d rules, which exceeds the %d-rule limit",
			path, len(doc.Rules), maxUniquenessRules)
	}

	rules := make(map[string]UniquenessRule, len(doc.Rules))
	for _, r := range doc.Rules {
		if err := validateUniquenessRule(r); err != nil {
			return fmt.Errorf("uniqueness rules: %s: %w", path, err)
		}
		rules[r.Name] = r
	}

	gs.rulesMu.Lock()
	gs.uniquenessRules = rules
	gs.rulesMu.Unlock()
	return nil
}

// persistUniquenessRulesLocked writes the registry to rules.json.
//
// Follows the WAL rotation pattern (pkg/wal/fileutil.go's Rotate): open a
// temp file, write, fsync, close, rename over the target, then fsync the
// parent directory so the rename itself is durable. Caller must hold
// rulesMu for the duration, so two concurrent Register/Remove calls cannot
// interleave writes to the same file.
func (gs *GraphStorage) persistUniquenessRulesLocked() error {
	rules := make([]UniquenessRule, 0, len(gs.uniquenessRules))
	for _, r := range gs.uniquenessRules {
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })

	data, err := json.Marshal(uniquenessRulesDocument{Version: uniquenessRulesFileVersion, Rules: rules})
	if err != nil {
		return fmt.Errorf("uniqueness rules: marshal: %w", err)
	}

	path := uniquenessRulesPath(gs.dataDir)
	tmpPath := path + ".new"

	f, err := gs.fs.Open(tmpPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePermissions)
	if err != nil {
		return fmt.Errorf("uniqueness rules: open %s: %w", tmpPath, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("uniqueness rules: write %s: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("uniqueness rules: sync %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("uniqueness rules: close %s: %w", tmpPath, err)
	}
	if err := gs.fs.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("uniqueness rules: rename %s to %s: %w", tmpPath, path, err)
	}
	if err := vfs.SyncParentDir(gs.fs, path); err != nil {
		return fmt.Errorf("uniqueness rules: sync data directory after publishing %s: %w", path, err)
	}
	return nil
}

// RegisterUniquenessRule adds or updates a rule by name and persists the
// registry. Idempotent: registering the same name twice upserts rather than
// erroring. Refuses a NEW name once the registry already holds
// maxUniquenessRules entries; an upsert of an existing name is never
// refused on that ground (R6).
//
// Registry writes are exempt from the required-rule check. This method
// never calls CreateNodeWithUniquenessRulesForTenant and never reads
// requiredUniquenessRules, so a missing required rule cannot block the
// call that registers it (ADR 0001, the deadlock the second rejected
// design had).
func (gs *GraphStorage) RegisterUniquenessRule(r UniquenessRule) error {
	if err := validateUniquenessRule(r); err != nil {
		return err
	}

	gs.rulesMu.Lock()
	defer gs.rulesMu.Unlock()

	previous, hadPrevious := gs.uniquenessRules[r.Name]
	if !hadPrevious && len(gs.uniquenessRules) >= maxUniquenessRules {
		return fmt.Errorf("uniqueness rules: registering %q would exceed the %d-rule limit", r.Name, maxUniquenessRules)
	}

	gs.uniquenessRules[r.Name] = r
	if err := gs.persistUniquenessRulesLocked(); err != nil {
		if hadPrevious {
			gs.uniquenessRules[r.Name] = previous
		} else {
			delete(gs.uniquenessRules, r.Name)
		}
		return err
	}
	return nil
}

// RemoveUniquenessRule removes a rule by name and persists the registry.
// Removing a name that is not registered is not an error.
//
// Registry writes are exempt from the required-rule check. This method
// never calls CreateNodeWithUniquenessRulesForTenant and never reads
// requiredUniquenessRules, so a missing required rule cannot block the
// call that registers it (ADR 0001, the deadlock the second rejected
// design had).
func (gs *GraphStorage) RemoveUniquenessRule(name string) error {
	gs.rulesMu.Lock()
	defer gs.rulesMu.Unlock()

	previous, existed := gs.uniquenessRules[name]
	if !existed {
		return nil
	}
	delete(gs.uniquenessRules, name)

	if err := gs.persistUniquenessRulesLocked(); err != nil {
		gs.uniquenessRules[name] = previous
		return err
	}
	return nil
}

// UniquenessRules returns a copy of the registered rules, sorted by name.
func (gs *GraphStorage) UniquenessRules() []UniquenessRule {
	gs.rulesMu.RLock()
	defer gs.rulesMu.RUnlock()

	rules := make([]UniquenessRule, 0, len(gs.uniquenessRules))
	for _, r := range gs.uniquenessRules {
		rules = append(rules, r)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Name < rules[j].Name })
	return rules
}

// planUniquenessEnforcement reads the registry under rulesMu.RLock and
// returns everything CreateNodeWithUniquenessRulesForTenant needs as plain
// values, copied out before the lock releases via the deferred RUnlock.
// The caller never touches rulesMu itself, so no early return added to
// CreateNodeWithUniquenessRulesForTenant in the future can leak the read
// lock (fix round 1, review finding).
//
// Exactly one of the two return values is meaningful: a non-nil missing
// means the whole call must be refused before any primitive runs; a nil
// missing means matched holds every registered rule whose Label is among
// the write's labels (zero, one, or more).
//
// R9 (controller ruling, fix round 1): a required (Name, Label) pair is
// satisfied only by a REGISTERED rule carrying BOTH the same name AND the
// same label. A rule registered under the required name but a DIFFERENT
// label does not satisfy the pair — the pair, not the name alone, is what
// fail-closed checks against, so a required "claim_for_task"/"Claim" is
// still missing if "claim_for_task" is registered against "Task".
func (gs *GraphStorage) planUniquenessEnforcement(labels []string) (missing *RequiredRuleMissingError, matched []UniquenessRule) {
	gs.rulesMu.RLock()
	defer gs.rulesMu.RUnlock()

	var missingPairs []RequiredUniquenessRule
	for _, req := range gs.requiredUniquenessRules {
		if !containsString(labels, req.Label) {
			continue
		}
		rule, registered := gs.uniquenessRules[req.Name]
		if registered && rule.Label == req.Label {
			continue
		}
		missingPairs = append(missingPairs, req)
	}
	if len(missingPairs) > 0 {
		sort.Slice(missingPairs, func(i, j int) bool { return missingPairs[i].Name < missingPairs[j].Name })
		first := missingPairs[0]
		return &RequiredRuleMissingError{RuleName: first.Name, Label: first.Label}, nil
	}

	for _, rule := range gs.uniquenessRules {
		if containsString(labels, rule.Label) {
			matched = append(matched, rule)
		}
	}
	return nil, matched
}

// CreateNodeWithUniquenessRulesForTenant is the one lookup path both write
// surfaces (the GraphQL resolver and the REST handler, wired in stage 2)
// call for node creation. It replaces the earlier direct calls to
// CreateNodeWithUniquePropertyForTenant that hardcoded a single (label,
// propertyKey) pair, so graphdb ships no domain vocabulary and both
// surfaces enforce identically (ADR 0001).
//
// Order of checks, matching the ADR's fail-closed-per-deployment design;
// see planUniquenessEnforcement for the registry lookup itself:
//
//  1. Any StorageConfig.RequiredUniquenessRules pair covering labels and
//     not satisfied by a matching registered rule (R9: same name AND same
//     label) refuses the whole call with *RequiredRuleMissingError, naming
//     the first such pair by name. No storage is touched.
//  2. Among the REGISTERED rules, any whose Label is in labels is a match.
//     Zero matches: an ordinary create. One match: the property it names
//     must be present — its absence refuses with
//     ErrUniquenessRulePropertyMissing, a client error stage 2's write
//     surfaces map to HTTP 400 — then the create runs through
//     CreateNodeWithUniquePropertyForTenant. More than one match:
//     ErrMultipleUniquenessRules, because the underlying primitive
//     enforces exactly one (label, propertyKey) pair per call and a silent
//     choice between two rules is the failure class the ADR exists to
//     avoid (R7).
//
// This method never holds rulesMu directly — planUniquenessEnforcement
// does, and releases it before returning — so every branch below is free
// to call a gs.mu-taking primitive without re-entering gs.mu.
func (gs *GraphStorage) CreateNodeWithUniquenessRulesForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
) (*Node, error) {
	missing, matched := gs.planUniquenessEnforcement(labels)
	if missing != nil {
		return nil, missing
	}

	switch len(matched) {
	case 0:
		return gs.CreateNodeWithTenant(tenantID, labels, properties)
	case 1:
		rule := matched[0]
		if _, ok := properties[rule.PropertyKey]; !ok {
			return nil, fmt.Errorf("%w: label %q requires a %q property", ErrUniquenessRulePropertyMissing, rule.Label, rule.PropertyKey)
		}
		return gs.CreateNodeWithUniquePropertyForTenant(tenantID, labels, properties, rule.Label, rule.PropertyKey)
	default:
		sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
		return nil, fmt.Errorf("%w: %q and %q", ErrMultipleUniquenessRules, matched[0].Name, matched[1].Name)
	}
}
