package tools

import "context"

// Tool is the interface every agent tool must satisfy
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, params map[string]interface{}) (string, error)
}
