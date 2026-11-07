# A mutex section that calls a function between Lock and an explicit Unlock.
# If the call panics, the Unlock never runs and the lock stays held; a deferred
# Unlock runs during the panic. graphdb #665 moved such sections to defer.
#
# Short sections with no call (mu.Lock(); n++; mu.Unlock()) cannot panic, so the
# rule leaves them alone. Builtins and conversions are not calls for this rule;
# close and panic stay calls, because they can panic.
#
# Limits: only a call that is a statement in the same block as Lock and Unlock
# is seen. A call in a nested block (if, for) or inside a larger expression
# (n += s.size()) is not.
PAIRS = [("Lock", "Unlock"), ("RLock", "RUnlock")]
LHS = ["$A", "$A, $B", "$A, $B, $C", "$A, $B, $C, $D"]
CALLS = ["$F(...)", "return $F(...)"] + ["%s %s $F(...)" % (l, op) for l in LHS for op in [":=", "="]]
NOT_CALLS = [
    "append", "cap", "clear", "complex", "copy", "delete", "imag", "len", "make",
    "max", "min", "new", "print", "println", "real",
    "bool", "byte", "rune", "string", "error", "any",
    "int", "int8", "int16", "int32", "int64",
    "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
    "float32", "float64",
]

def is_call(b):
    f = b["F"].text
    return f not in NOT_CALLS and not f.startswith("[]") and not f.startswith("map[")

RULES = [
    snare.pattern(
        id = "graphdb-lock-call-without-defer",
        severity = "medium",
        message = "$M is locked while $F is called, and the unlock is not deferred: a panic in $F leaves $M locked",
        languages = ["go"],
        match = snare.any(*["...\n$M.%s()\n...\n%s\n...\n$M.%s()\n..." % (l, c, u) for (l, u) in PAIRS for c in CALLS]),
        where = is_call,
        metadata = {"cwe": "CWE-667"},
    ),
]
