p = "scripts/check_concept_budget.py"
s = open(p).read()
renames = {
    evolution/refine.go:RefineRecord: evolution/refine/refine.go:RefineRecord,
    evolution/refine.go:Refine: evolution/refine/refine.go:Refine,
    memory/projection.go:ChainFact: memory/replay/projection.go:ChainFact,
    memory/projection.go:RebuildReport: memory/replay/projection.go:RebuildReport,
    memory/segment_store.go:SegmentRecord: memory/ledger/segment_store.go:SegmentRecord,
    memory/curation.go:CurationArtifact: evolution/meditation/vocabulary.go:MeditationArtifact,
    build.go:Deps: tool/subagent/subagent.go:Definition,
}
for o, n in renames.items():
    if o in s:
        s = s.replace(o, n, 1)
anchor = "BASELINE = ("
i = s.index(anchor)
add = """
    # Z1/Z2 package moves: registered keys (data-borne records, same exemption class)
    ("struct-fields", "evolution/meditation/vocabulary.go:MeditationArtifact", 6),
    ("struct-fields", "evolution/refine/refine.go:RefineRecord", 7),
    ("struct-fields", "memory/ledger/segment_store.go:SegmentRecord", 6),
    ("struct-fields", "memory/replay/projection.go:ChainFact", 6),
    ("struct-fields", "memory/replay/projection.go:RebuildReport", 9),
    ("struct-fields", "tool/subagent/subagent.go:Definition", 7),"""
s = s[: i + len(anchor)] + add + s[i + len(anchor) :]
open(p, "w").write(s)
print("baseline rewritten")
