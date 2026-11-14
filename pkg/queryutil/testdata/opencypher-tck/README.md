# openCypher TCK subset

The `.feature` files in this directory are a subset of the openCypher
Technology Compatibility Kit (TCK).

- Source repository: https://github.com/opencypher/openCypher
- Revision: `1a5aa1031a`
- Licence: Apache License 2.0 (see `LICENSE` in this directory)
- The files are not changed. They are byte-for-byte copies of the files at
  that revision. Do not edit them. To update the subset, copy new files from a
  named revision and record the revision here.

The harness in `pkg/queryutil/tck_harness_test.go` runs the scenarios and
compares each outcome with `pkg/queryutil/testdata/tck_baseline.tsv`.
