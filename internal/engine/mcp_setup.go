package engine

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/robertkohahimn/nanocode/internal/config"
	"github.com/robertkohahimn/nanocode/internal/mcp"
	"github.com/robertkohahimn/nanocode/internal/tool"
)

const mcpStartupTimeout = 15 * time.Second

// loadMCPTools initializes MCP servers and returns their tools and closers.
func loadMCPTools(cfg *config.Config) ([]tool.Tool, []io.Closer) {
	var allTools []tool.Tool
	var clients []io.Closer

	for name, serverCfg := range cfg.MCPServers {
		var mcpTools []tool.Tool
		switch serverCfg.Transport {
		case "stdio":
			client, err := mcp.NewStdioClient(serverCfg.Command, serverCfg.Args, serverCfg.Env)
			if err != nil {
				log.Printf("mcp: failed to start %s: %v", name, err)
				continue
			}
			initCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			err = client.Initialize(initCtx)
			cancel()
			if err != nil {
				client.Close()
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			listCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			tools, err := client.ListTools(listCtx)
			cancel()
			if err != nil {
				client.Close()
				log.Printf("mcp: failed to list tools from %s: %v", name, err)
				continue
			}
			mcpTools = client.Tools(name+"_", tools)
			clients = append(clients, client)
		case "http":
			client := mcp.NewHTTPClient(serverCfg.URL)
			initCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			err := client.Initialize(initCtx)
			cancel()
			if err != nil {
				log.Printf("mcp: failed to initialize %s: %v", name, err)
				continue
			}
			listCtx, cancel := context.WithTimeout(context.Background(), mcpStartupTimeout)
			tools, err := client.ListTools(listCtx)
			cancel()
			if err != nil {
				log.Printf("mcp: failed to list tools from %s: %v", name, err)
				continue
			}
			mcpTools = client.Tools(name+"_", tools)
		default:
			log.Printf("mcp: unknown transport %q for server %s", serverCfg.Transport, name)
			continue
		}
		allTools = append(allTools, mcpTools...)
	}

	return allTools, clients
}
