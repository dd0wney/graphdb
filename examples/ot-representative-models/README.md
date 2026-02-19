# OT Representative Models

Representative network models demonstrating betweenness centrality analysis for operational technology (OT) security. Part of the book *"Protecting Critical Infrastructure"* by Darragh Downey.

## Overview

These models demonstrate the "invisible node" problem in critical infrastructure security. Traditional network diagrams show technical systems (PLCs, SCADA servers, firewalls), but miss the human dependencies and organisational processes that create catastrophic single points of failure.

**Key concept:** Betweenness centrality measures how often a node appears on shortest paths between other nodes. High BC indicates a node is critical for network connectivity. When humans or processes have higher BC than technical systems, they represent invisible vulnerabilities.

## The Models

### Model 1: Steve's Utility (33 nodes, 70 edges)

A mid-sized utility demonstrating the "Steve Problem". One helpful senior OT technician has accumulated cross-domain access over 20 years, creating an invisible single point of failure.

**Expected results:**
- Steve BC: 0.6682 (rank #1)
- SCADA_Server BC: 0.1486 (rank #2)
- Steve vs top technical: 4.50x
- Invisible node BC share: 68.6%

### Model 2: Chemical Facility (24 nodes, 37 edges)

Chemical processing facility with Safety Instrumented System (SIS), Distributed Control System (DCS), and corporate IT. Demonstrates how the IT/OT coordinator role becomes a bridge concentration point.

**Expected results:**
- IT_OT_Coord BC: 0.3241 (rank #3)
- DMZ_Firewall BC: 0.1462 (rank #6)
- Bridge person vs firewall: 2.22x

### Model 3: Water Treatment VLAN Comparison

Two versions of a water treatment facility:
- **3a (Flat):** 13 nodes, three switches in full mesh
- **3b (VLAN):** 14 nodes, star topology through L3 core switch

Demonstrates how VLAN segmentation, while improving security, concentrates BC on the core switch.

**Expected results:**
- Flat max switch BC: 0.5758
- VLAN max switch BC: 0.7692
- VLAN increases max BC by 1.34x

### Model 4: Telecommunications Provider (114 nodes, 253 edges)

A telecommunications provider with cross-sector critical infrastructure dependencies. Demonstrates that the invisible node pattern scales to realistic network complexity. The telecom sector is the "infrastructure of infrastructures" with banking, emergency services, healthcare, transport, and energy sectors depending on it.

**Node types:**
- Technical: 66 nodes (routers, switches, gateways, servers)
- Human: 22 nodes (engineers, operators, managers, vendors)
- Process: 8 nodes (CAB, incident management, compliance)
- External: 18 nodes (dependent sector infrastructure)

**Expected results:**
- Core_Router_SYD BC: 0.4346 (rank #1)
- Senior_Network_Eng BC: 0.4058 (rank #2)
- Senior eng vs core router: 0.93x (human nearly equals backbone hub)
- Invisible node BC share: 34.5%

**Senior Engineer Removal Analysis:**
- Core_Router_SYD: +45% BC increase
- Change_Advisory_Board: +848% BC increase
- Network_Director: +784% BC increase

**Cascade Failure Analysis:**
- Core_Router_SYD failure disconnects 2 sectors (Energy, Transport)
- Each sector gateway is a single point of failure for its dependent infrastructure

## Running

```bash
cd examples/ot-representative-models
go run .
```

Output includes:
- BC rankings for each model (top 15-20 nodes)
- Steve removal analysis (before/after comparison)
- VLAN comparison analysis
- Telecom gateway and cascade failure analysis
- Senior engineer removal analysis
- JSON export to `results.json`

## Verifying Results

The results can be independently verified using Python/NetworkX:

```python
import networkx as nx

# Create graph
G = nx.Graph()
G.add_edges_from([
    ("Steve", "SCADA_Server"),
    ("Steve", "PLC_Turbine1"),
    # ... (see EDGE_DEFINITIONS.md for complete list)
])

# Compute betweenness centrality
bc = nx.betweenness_centrality(G, normalized=True)

# Print top results
for node, value in sorted(bc.items(), key=lambda x: -x[1])[:10]:
    print(f"{node}: {value:.4f}")
```

## Architecture Notes

### Undirected Graph Simulation

The cluso-graphdb `BetweennessCentrality` algorithm uses `GetOutgoingEdges()` for BFS traversal. To simulate undirected graphs (matching NetworkX behaviour), all edges are created in both directions:

```go
func createUndirectedEdge(graph *storage.GraphStorage, fromID, toID uint64, edgeType string, props map[string]storage.Value) error {
    graph.CreateEdge(fromID, toID, edgeType, props, 1.0)
    graph.CreateEdge(toID, fromID, edgeType, props, 1.0)
    return nil
}
```

### Normalisation

NetworkX uses normalisation factor `2/((n-1)(n-2))` for undirected graphs. cluso-graphdb uses `1/((n-1)(n-2))`. The code auto-detects and corrects for any discrepancy by checking Steve's BC against the expected value.

## Important Disclaimer

These are **representative models** based on common OT architectural patterns observed across a decade of operational technology experience. They are **NOT empirical measurements** from specific facilities. The models illustrate typical vulnerability patterns, not actual infrastructure.

## Files

- `main.go` - Entry point, runs all models and analysis
- `models.go` - Graph construction functions for Models 1-3
- `analysis.go` - BC analysis, comparison, and export functions
- `telecom_model.go` - Model 4 graph construction (114 nodes, 253 edges)
- `telecom_analysis.go` - Telecom-specific analysis (gateway BC, cascade failures)
- `TASK.md` - Implementation task description for Models 1-3
- `TASK_MODEL4.md` - Implementation task description for Model 4
- `EDGE_DEFINITIONS.md` - Complete edge lists for Models 1-3
- `NODE_DEFINITIONS.md` - Complete node definitions for Models 1-3
- `TELECOM_EDGES.md` - Complete edge lists for Model 4 (253 edges)
- `TELECOM_NODES.md` - Complete node definitions for Model 4 (114 nodes)
- `results.json` - JSON export of analysis results (generated at runtime)

## Connection to the Book

This code supports Chapter X of *"Protecting Critical Infrastructure"*, which introduces the GT-SMDN (Game-Theoretic Semi-Markov Decision Network) framework. The framework uses betweenness centrality to identify invisible nodes that create systemic risk in critical infrastructure.

The key insight: **traditional security focuses on technical systems, but organisational vulnerabilities (human dependencies, informal processes, accumulated access) often have higher impact potential.**

## Author

Darragh Downey
github.com/dd0wney
