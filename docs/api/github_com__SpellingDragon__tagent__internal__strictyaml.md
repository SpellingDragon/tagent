package strictyaml // import "github.com/SpellingDragon/tagent/internal/strictyaml"

Package strictyaml 是所有配置入口共用的**唯一**严格解码实现：未知字段一律让加载明确
失败，而不是静默忽略一个拼错的关键字。一处实现、多处调用——新增配置入口必须走本包， 不得另立第二套严格度。

FUNCTIONS

func DecodeByExt(path string, data []byte, out any) error
    DecodeByExt 按扩展名分派（.yaml/.yml 走 YAML，其余走 JSON），与 LoadConfig 的格式自动识别 保持一致。

func DecodeJSON(data []byte, out any) error
    DecodeJSON 严格解析 JSON：拒绝未知字段，并拒绝首个值之后的任何尾随内容。

func DecodeYAML(data []byte, out any) error
    DecodeYAML 严格解析 YAML：拒绝未知字段，并拒绝首个文档之后的任何尾随文档——静默忽略 第二个文档会让用户以为其中的配置已经生效。
