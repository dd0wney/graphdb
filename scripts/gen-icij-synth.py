#!/usr/bin/env python3
"""Synthetic ICIJ-shaped corpus generator for consumer-drive.sh.

Emits nodes.csv + edges.csv in the schema cmd/import-icij expects. Plants a clean 2-hop
conflict (two named Officers sharing one Entity) and >maxDegree hubs. Deterministic (seeded).
Usage: gen-icij-synth.py <outdir>
"""
import csv
import os
import random
import sys

random.seed(1729)
N_ENTITY, N_OFFICER, N_INTERMEDIARY, N_ADDRESS = 20000, 18000, 2000, 10000
JURIS = ["BVI", "PAN", "CYM", "JEY", "BMU", "SAM", "SYC", "MLT"]
outdir = sys.argv[1] if len(sys.argv) > 1 else "/tmp/icij-synth"

nodes, nid = [], 0
def add(name, ntype, juris=""):
    global nid
    nid += 1
    nodes.append((str(nid), name, juris, ntype))
    return str(nid)

acme = add("Acme Holdings Ltd", "Entity", "BVI")
smith = add("Robert Smith", "Officer")
doe = add("Jane Doe", "Officer")
entities = [acme] + [add(f"Entity {i} Ltd", "Entity", random.choice(JURIS)) for i in range(N_ENTITY - 1)]
officers = [smith, doe] + [add(f"Officer Person {i}", "Officer") for i in range(N_OFFICER - 2)]
intermediaries = [add(f"Law Firm {i}", "Intermediary", random.choice(JURIS)) for i in range(N_INTERMEDIARY)]
addresses = [add(f"{i} Offshore Plaza", "Address") for i in range(N_ADDRESS)]

edges = []
def edge(rt, a, b):
    edges.append((rt, a, b))

edge("officer_of", smith, acme)
edge("officer_of", doe, acme)
for off in officers[2:]:
    for _ in range(random.randint(1, 2)):
        edge("officer_of", off, random.choice(entities))
for inter in intermediaries[2:]:
    for _ in range(random.randint(1, 4)):
        edge("intermediary_of", inter, random.choice(entities))
for hub in intermediaries[:2]:
    for ent in random.sample(entities, 3000):
        edge("intermediary_of", hub, ent)
for ent in entities:
    edge("registered_address", ent, random.choice(addresses))
for ent in random.sample(entities, 3000):
    edge("registered_address", ent, addresses[0])

os.makedirs(outdir, exist_ok=True)
with open(f"{outdir}/nodes.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["node_id", "name", "jurisdiction", "country_codes", "countries", "node_type", "sourceID", "address", "valid_until", "note"])
    for (i, name, juris, ntype) in nodes:
        w.writerow([i, name, juris, "", "", ntype, "synthetic", "", "", ""])
with open(f"{outdir}/edges.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["rel_type", "node_id_start", "node_id_end", "link", "status", "start_date", "end_date"])
    for (rt, a, b) in edges:
        w.writerow([rt, a, b, rt, "", "", ""])

print(f"nodes={len(nodes)} edges={len(edges)} -> {outdir}")
print(f"planted: Acme={acme} Smith={smith} Doe={doe} (both officer_of Acme)")
