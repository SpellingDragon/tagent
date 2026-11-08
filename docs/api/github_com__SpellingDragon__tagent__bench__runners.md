package runners // import "github.com/SpellingDragon/tagent/bench/runners"

Package runners owns the three live-benchmark runners over one Task interface.

Baseline (1.3), tagent's bench-bridge subprocess (1.4) and DeepSeek Harness
(1.4) land here. The package deliberately exports no named type yet: the runner
contract is bench.Task and this package may import bench, never the reverse.

FUNCTIONS

func BaselineToolFace() []string
    BaselineToolFace returns the tool names the baseline runner exposes.

func BuildBridge(dst string) (string, error)
    BuildBridge builds the wechat-bot bench-bridge into dst and returns its
    path.

func CapabilityMap() []byte
    CapabilityMap returns the registered capability-to-tool mapping, the table a
    runner's advertised tool face is judged against.

func DSHExecutable(checkout string) (string, []string, error)
    DSHExecutable resolves the node program and CLI argument the adapter runs.

func DSHToolFace() []string
    DSHToolFace returns the DeepSeek Harness tool names the adapter maps onto.

func RunComparison(runRoot string, repeats int) (string, error)
    RunComparison runs baseline, tagent and DeepSeek Harness over the embedded
    task set and writes one run directory under runRoot. It returns the run
    directory; a skipped run (no API key) is written and returned without error.

func NewModelRelay(upstream string) (*modelRelay, error)
    NewModelRelay starts a loopback relay in front of upstream.

func NewBaseline(endpoint, apiKeyEnv, modelName string) (runner, error)
    NewBaseline returns a runner over a plain trpc-agent-go llmagent loop.

func NewDSH(program string, arguments []string, endpoint, apiKeyEnv, modelName string) (runner, error)
    NewDSH returns a runner over the headless CLI at program arguments.

func NewTagent(bridge, wechatConfig, endpoint, apiKeyEnv, modelName string) (runner, error)
    NewTagent returns a runner over the bench-bridge subprocess.
