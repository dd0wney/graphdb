# Two mutexes taken in both orders: one path holds A while it takes B, and
# another holds B while it takes A. Run at the same time, each waits for the
# other for ever. graphdb #726 fixed lsm GetStats, which took stats.mu then
# mu while flush and compact took mu then stats.mu.
#
# Written by the snare session. Needs snare 0895313 or later (snare.lockorder,
# snare #79); an older binary exits 2 on this file.
#
# Limits (snare v1): intraprocedural only, so holding A while calling a helper
# that takes B adds no edge; a release through a helper is not seen; two
# instances of one type count as one mutex, and the self-edge is ignored.
RULES = [
    snare.lockorder(
        id = "graphdb-lock-order-inversion",
        severity = "high",
        message = "Two mutexes are taken in both orders; this can deadlock",
        languages = ["go"],
    ),
]
