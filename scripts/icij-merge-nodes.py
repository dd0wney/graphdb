#!/usr/bin/env python3
"""Merge the ICIJ full-oldb node files into the single nodes.csv that
cmd/import-icij reads.

The ICIJ "full-oldb" package ships one CSV per node type (nodes-entities.csv,
nodes-officers.csv, nodes-intermediaries.csv, nodes-addresses.csv,
nodes-others.csv) with a different header in each, and no node_type column.
import-icij keys its columns by header name and takes the label from
node_type, so this script supplies node_type from the file name and projects
every file onto the importer's ten columns. relationships.csv already has the
node_id_start / node_id_end / rel_type header the importer expects.

Usage:
    python3 scripts/icij-merge-nodes.py <unzipped full-oldb dir> <out nodes.csv>

Measured on the 2023-09-06 package: 2,017,662 rows in ~10 s.
"""
import csv
import os
import sys

COLUMNS = [
    "node_id", "name", "jurisdiction", "country_codes", "countries",
    "node_type", "sourceID", "address", "valid_until", "note",
]
FILES = [
    ("nodes-entities.csv", "Entity"),
    ("nodes-officers.csv", "Officer"),
    ("nodes-intermediaries.csv", "Intermediary"),
    ("nodes-addresses.csv", "Address"),
    ("nodes-others.csv", "Other"),
]


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    src, out = sys.argv[1], sys.argv[2]
    csv.field_size_limit(1 << 30)
    counts = {}
    with open(out, "w", newline="", encoding="utf-8") as fo:
        writer = csv.DictWriter(fo, fieldnames=COLUMNS, extrasaction="ignore")
        writer.writeheader()
        for name, label in FILES:
            path = os.path.join(src, name)
            if not os.path.exists(path):
                print(f"missing {path}", file=sys.stderr)
                return 1
            n = 0
            with open(path, newline="", encoding="utf-8") as fi:
                for row in csv.DictReader(fi):
                    row["node_type"] = label
                    writer.writerow(row)
                    n += 1
            counts[label] = n
    print({**counts, "total": sum(counts.values())})
    return 0


if __name__ == "__main__":
    sys.exit(main())
