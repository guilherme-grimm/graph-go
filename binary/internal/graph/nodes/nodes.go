package nodes

type NodeType string

const (
	TypePostgres   NodeType = "postgres"
	TypeMongodb    NodeType = "mongodb"
	TypeRedis      NodeType = "redis"
	TypeS3         NodeType = "s3"
	TypeDatabase   NodeType = "database"
	TypeTable      NodeType = "table"
	TypeCollection NodeType = "collection"
	TypeBucket     NodeType = "bucket"
	TypeStorage    NodeType = "storage"
)

type Node struct {
	Id       string         `json:"id"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Parent   string         `json:"parent,omitempty"`
	Metadata map[string]any `json:"metadata"`
	Health   string         `json:"health"`
}
