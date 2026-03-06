# Examples

Worked examples demonstrating cluso-graphdb capabilities across different domains.

## Critical Infrastructure Models

These examples accompany the book *"Protecting Critical Infrastructure"* by Darragh Downey. They use raw graph operations to model real-world incidents and analyse structural vulnerabilities via betweenness centrality, cascade failure simulation, and blast radius analysis.

| Example | Incident / Domain | Book Reference |
|---------|-------------------|----------------|
| [pipeline-ransomware](pipeline-ransomware/) | Colonial Pipeline (2021) | Model 7 |
| [power-grid-cascade](power-grid-cascade/) | Ukraine Grid (2015-2016) | Model 6 |
| [water-treatment-attack](water-treatment-attack/) | Oldsmar, Florida (2021) | Model 5 |
| [hospital-network](hospital-network/) | NHS WannaCry (2017) | Model 8 |
| [juniper-zeroize](juniper-zeroize/) | Volt Typhoon APT pre-positioning | Model 9 |
| [ot-representative-models](ot-representative-models/) | Multi-sector OT patterns | Models 1-4 |

## Library Demos

Generic examples showcasing graphdb features without domain-specific modelling.

| Example | Feature |
|---------|---------|
| [constraint-validation](constraint-validation/) | Property and cardinality constraints |
| [cycle-detection](cycle-detection/) | Routing loop detection |
| [iso15288-system](iso15288-system/) | Systems engineering framework modelling |

## GT-SMDN Platform

The production implementation of the GT-SMDN framework described in the book lives in a separate repository: [gt-smdn-platform](https://github.com/oit-cyber/gt-smdn-platform). It builds on cluso-graphdb with typed domain models, cascade probability formulas, Shannon entropy scoring, compliance integration, and a full web interface.

These examples use raw graphdb operations to illustrate individual concepts; gt-smdn-platform is the integrated system.
