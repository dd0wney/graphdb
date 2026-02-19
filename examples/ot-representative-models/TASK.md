# Claude Code Task: Build Representative OT Network Models in cluso-graphdb

## Context

I am writing a technical book, "Protecting Critical Infrastructure," which introduces the GT-SMDN (Game-Theoretic Semi-Markov Decision Network) framework for OT/ICS security. The book uses betweenness centrality to identify "invisible nodes" (human dependencies, processes, organisational vulnerabilities) that don't appear on traditional network diagrams but create catastrophic single points of failure.

The book needs **representative network models** with **real, reproducible betweenness centrality calculations** to demonstrate the framework. These models are based on common OT architectural patterns I have observed across a decade of operational technology experience. They are NOT empirical measurements from specific facilities.

I have already validated the expected outputs using Python/NetworkX. The task here is to implement these same models in cluso-graphdb so the book can reference the author's own software producing these results.

Reference files in this directory:
- `EDGE_DEFINITIONS.md` — Complete edge lists for all models (copy exactly)
- `NODE_DEFINITIONS.md` — Complete node lists with labels and properties

## Repository

**Location:** `/Users/darraghdowney/Workspace/github.com/cluso-graphdb`
**Language:** Go
**Module:** `github.com/dd0wney/cluso-graphdb`

## What to Build

Build the following files in THIS directory (`examples/ot-representative-models/`):

1. `main.go` — Main program that builds all three models and runs analysis
2. `models.go` — Model definitions (graph construction functions)
3. `analysis.go` — Analysis functions (BC calculation, comparison, removal analysis)
4. `README.md` — Documentation explaining the models and how to run them

## Critical API Details

### Initialisation
```go
graph, err := storage.NewGraphStorage("./data/ot_models")
if err != nil {
    log.Fatalf("Failed to create graph: %v", err)
}
defer graph.Close()
```

### Creating Nodes
```go
node, err := graph.CreateNode(
    []string{"Human", "Operator"},           // Labels (used for categorisation)
    map[string]storage.Value{                 // Properties
        "name":      storage.StringValue("Steve"),
        "role":      storage.StringValue("Senior OT Technician"),
        "level":     storage.StringValue("Human"),
        "node_type": storage.StringValue("human"),
    },
)
```

### Creating Edges
```go
edge, err := graph.CreateEdge(
    fromNode.ID,        // uint64
    toNode.ID,          // uint64
    "HUMAN_ACCESS",     // Edge type string
    map[string]storage.Value{
        "description": storage.StringValue("Admin access to SCADA"),
    },
    1.0,                // Weight (float64)
)
```

### ⚠️ CRITICAL: Undirected Graph Simulation

The `BetweennessCentrality` algorithm in `pkg/algorithms/centrality.go` uses `graph.GetOutgoingEdges(v)` for BFS traversal. This means it only follows OUTGOING edges. For undirected graph behaviour (which is what the NetworkX models use), **you must create edges in BOTH directions** for every connection:

```go
// For undirected edge between A and B:
graph.CreateEdge(nodeA.ID, nodeB.ID, "TECHNICAL", props, 1.0)
graph.CreateEdge(nodeB.ID, nodeA.ID, "TECHNICAL", props, 1.0)
```

Write a helper function:
```go
func createUndirectedEdge(graph *storage.GraphStorage, fromID, toID uint64, edgeType string, props map[string]storage.Value) error {
    _, err := graph.CreateEdge(fromID, toID, edgeType, props, 1.0)
    if err != nil {
        return err
    }
    _, err = graph.CreateEdge(toID, fromID, edgeType, props, 1.0)
    return err
}
```

### Running Betweenness Centrality
```go
import "github.com/dd0wney/cluso-graphdb/pkg/algorithms"

bc, err := algorithms.BetweennessCentrality(graph)
// bc is map[uint64]float64 — node ID to normalised BC score
```

### Getting Node Names from IDs
Maintain a `map[uint64]string` mapping node IDs to names, since BC returns uint64 IDs:
```go
nodeNames := make(map[uint64]string)
// After creating each node:
nodeNames[node.ID] = "Steve"
```

Also maintain a `map[uint64]string` for node_type ("human", "technical", "process") and level.

## The Three Models

### Model 1: Steve's Utility (33 nodes, 70 undirected edges)

A mid-sized utility demonstrating the Steve Problem. One helpful senior OT technician has accumulated cross-domain access, creating an invisible single point of failure.

**Node categories:**
- Level 0 (Process): PLC_Turbine1, PLC_Turbine2, PLC_Substation, RTU_Remote1, RTU_Remote2
- Level 1 (Control): HMI_Control1, HMI_Control2, Safety_PLC
- Level 2 (Supervisory): SCADA_Server, Historian_OT, Eng_Workstation
- Level 3 (Site Ops): OT_Switch_Core, Patch_Server, AD_Server_OT
- Level 3.5 (DMZ): Firewall_ITOT, Jump_Server, Data_Diode
- Level 4 (IT): IT_Switch_Core, Email_Server, ERP_System, AD_Server_IT, VPN_Gateway
- Human: Steve, OT_Manager, IT_Admin, Control_Op1, Control_Op2, Plant_Manager, Vendor_Rep
- Process: Change_Mgmt_Process, Incident_Response, Vendor_Access_Process, Patch_Approval

**Edge definitions:** Copy EXACTLY from `EDGE_DEFINITIONS.md` (Model 1 section). All edges must be created as undirected (both directions). Node labels and properties are in `NODE_DEFINITIONS.md`.

**Expected results (from NetworkX validation):**
- Steve BC: 0.6682 (rank #1)
- SCADA_Server BC: 0.1486 (rank #2)
- Steve vs top technical: 4.50x
- Invisible node BC share: 68.6%

### Model 2: Chemical Facility (24 nodes, 37 undirected edges)

Chemical processing with SIS, DCS, corporate IT. Demonstrates IT/OT bridge concentration.

**Node categories:**
- Safety: SIS_Controller, SIS_Logic_Solver, ESD_Panel
- DCS: DCS_Controller1, DCS_Controller2, DCS_Server, Op_Console1, Op_Console2
- Site: OT_Firewall, Historian, MES_Server, Eng_Station
- DMZ: DMZ_Firewall, Patch_Relay, Remote_Access
- Corporate: Corp_Firewall, Corp_Network, ERP, Internet_GW
- Human: DCS_Engineer, Process_Operator, Safety_Engineer, IT_OT_Coord, Site_IT

**Edge definitions:** Copy from `EDGE_DEFINITIONS.md` (Model 2 section).

**Expected results:**
- IT_OT_Coord BC: 0.3241 (rank #3)
- DMZ_Firewall BC: 0.1462 (rank #6)
- Bridge person vs firewall: 2.22x

### Model 3: Water Treatment VLAN Comparison

Two versions of the same facility: flat meshed network vs VLAN-segmented.

**Model 3a (Flat):** 13 nodes, 13 undirected edges. Three switches in full mesh.
**Model 3b (VLAN):** 14 nodes (adds L3_Core_Switch), 13 undirected edges. Star topology.

**Node and edge definitions:** Copy from `EDGE_DEFINITIONS.md` and `NODE_DEFINITIONS.md` (Model 3a/3b sections).

**Expected results:**
- Flat max switch BC: 0.5758
- VLAN max switch BC: 0.7692
- VLAN increases max BC by 1.34x

## Analysis Functions to Implement

### 1. BC Ranking Report
For each model, print sorted BC values with node name, type, level, and rank. Flag invisible nodes (human/process).

### 2. Steve Removal Analysis
Remove Steve from Model 1 and recalculate BC. This requires creating a SECOND graph instance without Steve and his edges. Show before/after comparison for the top 10 most-changed nodes.

### 3. VLAN Comparison
Run BC on both flat and VLAN versions. Compare max switch BC values. Calculate the ratio.

### 4. Summary Statistics
- Invisible node BC share (sum of human+process BC / total BC)
- Bridge risk multiplier (IT/OT Coord BC / DMZ Firewall BC)
- VLAN BC increase ratio

### 5. JSON Output
Export all BC results to `results.json` using `encoding/json`.

## Validation

The numbers from cluso-graphdb should match the NetworkX numbers within ±0.001. If they don't, two likely causes:

1. **Missing bidirectional edges** — the most common error. Every undirected connection needs two directed edges.

2. **Normalisation difference** — cluso-graphdb `centrality.go` uses `normFactor := 1.0 / float64((len(nodeIDs)-1)*(len(nodeIDs)-2))`. NetworkX uses `2/((n-1)(n-2))` for undirected graphs. Since we simulate undirected with bidirectional directed edges, the BFS counts paths in both directions.

    Check Steve's BC output:
    - If ~0.3341 (half of 0.6682): multiply all results by 2.0
    - If ~1.3364 (double): divide all results by 2.0
    - If ~0.6682: correct, no adjustment needed

## Output Format

Print results in clear tabular format matching the Python script output.

## Style Notes

- Australian/British English in all comments and output (organisation, analyse, normalised, behaviour, defence)
- Never use em-dashes (use commas, periods, or parentheses instead)
- Follow existing code style from `examples/iso15288-system/main.go`
- Each model function should return `(*storage.GraphStorage, map[uint64]string, map[uint64]string, map[uint64]string, error)` for (graph, nodeNames, nodeTypes, nodeLevels, error), or define a struct for model metadata

## Running

```bash
cd examples/ot-representative-models
go run .
```

Clean up data directory before each run: `os.RemoveAll("./data")` at start of main().
