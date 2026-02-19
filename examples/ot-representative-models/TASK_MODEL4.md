# Claude Code Task: Build Telecom Provider Model (Model 4) in cluso-graphdb

## Context

This is Model 4 in a series of representative OT network models for the book "Protecting Critical Infrastructure." Models 1-3 are already implemented in `examples/ot-representative-models/`.

Model 4 is a **telecommunications provider with cross-sector critical infrastructure dependencies** (114 nodes, 253 edges). It demonstrates that the GT-SMDN framework scales to realistic complexity and that the "invisible node" pattern (human nodes achieving disproportionate betweenness centrality) is an emergent property of complex systems, not an artefact of small graphs.

The telecom sector is the "infrastructure of infrastructures." Banking, emergency services, healthcare, transport, and energy all depend on it. This model captures those cascading dependencies.

I have validated expected outputs using Python/NetworkX. The task is to implement this model in cluso-graphdb (the existing Go example directory) so the book can reference the author's own software.

## Repository

**Location:** `/Users/darraghdowney/Workspace/github.com/cluso-graphdb`
**Module:** `github.com/dd0wney/cluso-graphdb`

## What to Build

Extend the EXISTING `examples/ot-representative-models/` directory. Add Model 4 alongside the three existing models.

### Files to Create/Modify:

1. **`telecom_model.go`** (NEW) — Model construction: `buildTelecomProvider()` function
2. **`telecom_analysis.go`** (NEW) — Telecom-specific analysis functions (cascade failure, senior engineer removal, gateway analysis)
3. **`main.go`** (MODIFY) — Add Model 4 execution after existing Models 1-3

### Reference Files (in this directory):
- `TELECOM_EDGES.md` — All 253 edges grouped by type (copy exactly)
- `TELECOM_NODES.md` — All 114 nodes with labels, levels, and types

## Critical Reminders (same as Models 1-3)

### Undirected Graph Simulation
Use the existing `createUndirectedEdge()` helper. Every edge needs BOTH directions.

### Separate Graph Instance
Model 4 needs its own `storage.NewGraphStorage("./data/telecom")` instance with its own data directory. Clean it up with `os.RemoveAll("./data/telecom")` before building.

### Node Metadata Tracking
Return the same metadata maps as existing models: `nodeNames`, `nodeTypes`, `nodeLevels`. Add `nodeFunctions` map for this model since nodes have a "function" property.

## Expected Results (from NetworkX validation)

### Top 20 by Betweenness Centrality:
```
#1  Core_Router_SYD          0.4346  technical  Core_Network
#2  Senior_Network_Eng       0.4058  human      Human           *** INVISIBLE
#3  Gateway_Emergency        0.1071  technical  Interconnection
#4  Gateway_Banking          0.0953  technical  Interconnection
#5  MME_Primary              0.0788  technical  Mobile_Core
#6  Gateway_Healthcare       0.0588  technical  Interconnection
#7  Exchange_CBD             0.0586  technical  Access_Network
#8  NMS_Primary              0.0573  technical  NOC
#9  Gateway_Transport        0.0526  technical  Interconnection
#10 Gateway_Energy           0.0526  technical  Interconnection
#11 Core_Router_MEL          0.0473  technical  Core_Network
#12 CTO                      0.0457  human      Human           *** INVISIBLE
#13 Corp_Firewall            0.0441  technical  Corporate_IT
#14 MME_Secondary            0.0431  technical  Mobile_Core
#15 Provisioning_System      0.0423  technical  BSS_OSS
#16 IP_Engineer              0.0383  human      Human           *** INVISIBLE
#17 NOC_Shift_Lead           0.0378  human      Human           *** INVISIBLE
#18 CAD_System               0.0352  external   Sector_Emergency
#19 Mobile_Engineer          0.0339  human      Human           *** INVISIBLE
#20 Banking_Liaison          0.0307  human      Human           *** INVISIBLE
```

### Key Numbers to Validate:
- **Total:** 114 nodes, 253 undirected edges
- **Core_Router_SYD BC:** 0.4346 (rank #1)
- **Senior_Network_Eng BC:** 0.4058 (rank #2)
- **Senior eng vs core router:** 0.93x (human nearly equals the backbone hub)
- **Invisible BC share:** 34.5% of total network BC
- **Graph diameter:** 6
- **Average shortest path:** 3.19

### Senior Engineer Removal (top 5 changes):
```
Core_Router_SYD:     0.4346 -> 0.6297  (+45%)
NMS_Primary:         0.0573 -> 0.1380  (+141%)
Change_Advisory_Board: 0.0068 -> 0.0643 (+848%)
Network_Director:    0.0045 -> 0.0396  (+784%)
IP_Engineer:         0.0383 -> 0.0728  (+90%)
```

### Cascade Failure:
- Core_Router_SYD failure disconnects **2 sectors** (Energy: 3 nodes, Transport: 3 nodes)
- Each sector gateway is a single point of failure for its sector

## Analysis Functions to Implement

### 1. Standard BC Ranking Report
Same format as Models 1-3. Print sorted BC values, flag invisible and external nodes.

### 2. Senior Engineer Removal Analysis
Create second graph without Senior_Network_Eng. Show BC redistribution for top 10 most-changed nodes with before/after/change/percentage.

### 3. Cross-Sector Gateway Analysis
For each `Gateway_*` node, print BC value and count of external sector nodes it serves.

### 4. Cascade Failure Analysis
For each internal node, test what happens if removed:
- Which external (sector) nodes become unreachable from `Core_Router_SYD` (or nearest surviving core router)?
- Group by sectors affected.
- Report nodes whose failure disconnects 2+ sectors.
- Report nodes whose failure disconnects exactly 1 sector.

This is computationally heavier (114 node removals x connectivity check each) but manageable. The existing BC algorithm handles 114 nodes in milliseconds.

### 5. Summary Statistics
Print:
- Node counts by type (technical/human/process/external)
- Invisible BC share
- Senior eng vs core router ratio
- Gateway BC rankings
- Cascade single points of failure

### 6. JSON Output
Add Model 4 results to the existing `results.json`, or create `telecom_results.json` alongside it.

## Node Type Categories

This model has FOUR node types (Models 1-3 only had three):
- `technical` — Routers, switches, servers, gateways (66 nodes)
- `human` — NOC staff, engineers, management, vendors (22 nodes)
- `process` — CAB, incident management, compliance (8 nodes)
- `external` — Dependent sector infrastructure (18 nodes)

"Invisible" nodes are human + process (30 nodes, 26.3% of total).
External nodes are NOT invisible (they represent real infrastructure, just outside the telecom boundary).

## Output Format

```
========================================================================
MODEL 4: TELECOMMUNICATIONS PROVIDER (114 nodes, 253 edges)
========================================================================

--- Node Type Breakdown ---
  technical: 66
  human: 22
  process: 8
  external: 18

--- Betweenness Centrality (normalised, top 20) ---
Rank  Node                          BC      Type         Level
------------------------------------------------------------------------
#1    Core_Router_SYD           0.4346  technical    Core_Network
#2    Senior_Network_Eng        0.4058  human        Human         *** INVISIBLE
...

--- Cross-Sector Gateway BC ---
  Gateway_Emergency            BC = 0.1071  (serves 3 external nodes)
  Gateway_Banking              BC = 0.0953  (serves 4 external nodes)
...

--- Cascade Failure Analysis ---
Nodes whose failure disconnects 2+ sectors:
  Core_Router_SYD: 2 sectors, 6 external nodes
    Sector_Energy: Grid_SCADA_Comms, Gas_Pipeline_Comms, Substation_Comms
    Sector_Transport: Rail_SCADA_Comms, Traffic_Mgmt_System, Port_Operations
```

## Style Notes (same as Models 1-3)

- Australian/British English (organisation, analyse, normalised, behaviour, defence)
- Never use em-dashes (use commas, periods, or parentheses instead)
- Follow existing code patterns from the Models 1-3 implementation
- Use separate graph instance with its own data directory
- Helper function `createUndirectedEdge()` is already defined in the existing code

## Testing

```bash
cd examples/ot-representative-models
go run .
```

Verify Senior_Network_Eng BC is approximately 0.4058. If it is approximately 0.2029 (half), the normalisation needs the same adjustment as Models 1-3 (which was NOT needed, so it should work without adjustment here too).
