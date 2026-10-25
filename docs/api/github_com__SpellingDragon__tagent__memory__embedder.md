package embedder // import "github.com/SpellingDragon/tagent/memory/embedder"

Package embedder assembles the provider-backed embedding connection of the
memory side from its declaration: the provider, the model, the environment
variable holding the key, the vector width and the optional endpoint. The
connection is validated at assembly time, so a host that runs is a host whose
vectors belong to the vector space its store was built for.

The deterministic embedding stand-in of the memory side: it maps text to a
fixed-dimension unit vector with no endpoint behind it, so the vector leg of
a retrieval runs to completion offline and a runtime without an embedding
configuration still exercises the whole retrieval mechanism end to end.
The vectors it carries are content fingerprints and not semantic measurements:
identical text always yields identical vectors and text sharing more character
n-grams scores a higher cosine. That is the mechanism guarantee a retrieval test
stands on, and no claim of semantic recall quality.

Skeleton compile-time assertions for the provider-backed embedder: the assembly
entry, the declaration check, the memory seam method and the named refusals keep
exactly these shapes.

CONSTANTS

const DefaultHashDimensions = 128
    DefaultHashDimensions is the vector width a HashEmbedder runs at when it
    names none.

VARIABLES

var ErrEmbeddingDeclaration = errors.New("memory/embedder: the embedding declaration is invalid")
    ErrEmbeddingDeclaration reports an embedding connection this package cannot
    assemble: an unknown provider, a connection missing its model, key variable
    or vector width, or an endpoint that is not an absolute http(s) URL.

var ErrEmbeddingKey = errors.New("memory/embedder: the embedding key is not set")
    ErrEmbeddingKey reports an embedding connection whose key variable names no
    value in the environment. The assembly refuses rather than send requests
    that are certain to be rejected, and names the variable that holds nothing.

var ErrEmbeddingUnwired = errors.New("memory/embedder: the embedding connection is not wired")
    ErrEmbeddingUnwired reports an embedder with no provider connection behind
    it: the zero value carries no vector source and answers no request.

var ErrEmptyText = errors.New("memory/embedder: embedder was handed empty text")
    ErrEmptyText names the refusal an embedder answers empty input with:
    an empty query keeps the retrieval on its keyword leg instead of paying
    doomed provider attempts.

FUNCTIONS

func Validate(spec config.EmbeddingSpec) error
    Validate reports whether one embedding declaration names everything its
    provider needs: a known provider, a model, the variable holding the key, a
    positive vector width, and an endpoint that is absolute when the declaration
    overrides the provider default.

TYPES

type Embedder struct {
	// Has unexported fields.
}
    Embedder adapts a provider embedder to the memory seam: it resolves the key
    variable once at assembly, converts the provider's float64 vector into the
    float32 the seam stores, and refuses an empty vector or one whose width is
    not the declared width by name rather than storing it into a vector space it
    does not belong to.

func New(spec config.EmbeddingSpec) (*Embedder, error)
    New assembles the embedding seam of one declared connection. The key is read
    at assembly and travels in the client, so a missing variable refuses the
    assembly by name before any request is prepared.

func (e *Embedder) Connection() config.EmbeddingSpec
    Connection reports the declared connection this embedder was assembled from,
    with the provider default endpoint filled in when the declaration named
    none.

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error)
    Embed returns the vector of text under the declared connection, honouring
    cancellation and answering a named failure when the provider fails, returns
    no vector, or returns a vector of another width.

type HashEmbedder struct {
	// Has unexported fields.
}
    HashEmbedder is the Embedder that hashes character n-grams into
    a fixed-dimension unit vector. The mapping is deterministic and
    honours cancellation, and the zero value is usable and runs at
    DefaultHashDimensions.

func NewHashEmbedder(dimensions int) *HashEmbedder
    NewHashEmbedder returns a hash embedder of the given vector width, or one at
    DefaultHashDimensions when the width names none.

func (e *HashEmbedder) Embed(ctx context.Context, text string) ([]float32, error)
    Embed returns the deterministic embedding vector of text: a unit vector over
    the character n-gram fingerprint of the text, identical for identical text.
