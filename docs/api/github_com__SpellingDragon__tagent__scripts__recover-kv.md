Command recover-kv verifies that every reference a journal root holds is
answered by the text of its original: a fact commit stages the reference alone
and commits the text inside the event body of the same key, so the chain is the
one place that text lives and the recall reads it back from there.

The command writes nothing. For every reference key of the root it reads
the copies the root holds of that text — the event body of the key, and the
independent body a bare commit staged beside the reference — and compares the
sha256 digest and byte length of each with the ones the references recorded.
A key the root holds no text for is named truly lost, a copy whose bytes are not
the ones the reference names is named a mismatch, and either one exits nonzero;
a root whose every reference is answered by the text it names exits zero.
The root is read through the one journal view that locates a key in the hot
index and in the archive manifest alike, so a text the hot files do not carry is
read from the archive root the operator names and an archive that cannot answer
is reported by name.

