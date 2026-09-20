// Command doc-processor is an example NEXUS agent that demonstrates the full
// stack: Core Runtime + HTTP Gateway + Agent + Tools.
//
// It processes "document processing" requests through the canonical chain:
//
//	OWNER REQUEST → VALIDATE → GOVERNANCE → OBJECTIVE → DECISION → PLAN
//	  → WORKFLOW → SCHEDULE → AGENT → TOOL → VERIFY → OUTCOME
//
// Usage:
//
//	go run ./examples/doc-processor
//	# → starts on :9090
//	# → POST /api/v1/requests with document processing intent
//
// Example request:
//
//	POST /api/v1/requests
//	{
//	  "intent": "process invoice document INV-2024-001",
//	  "business_id": "acme-corp",
//	  "actor_id": "user-1"
//	}
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/logging"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/gateway"
)

func main() {
	os.Exit(run())
}

func run() int {
	addr := ":9090"
	if v := os.Getenv("NEXUS_ADDR"); v != "" {
		addr = v
	}

	logger := logging.New(logging.Options{})
	life := lifecycle.New(lifecycle.Options{
		ShutdownTimeout: 10 * time.Second,
	})
	healthSrv := health.NewServer()
	_ = healthSrv // Used by lifecycle

	// Create the core engine
	engine := core.NewEngine(&config.Config{})

	// Register document processing tools
	toolReg := engine.ToolRegistry()
	registerDocTools(toolReg, logger)

	// Provision the document processor agent
	def := &agent.AgentDefinition{
		Name:        "doc-processor",
		Version:     "1.0.0",
		Description: "Processes documents through the NEXUS canonical chain",
		Type:        agent.AgentTypeWorker,
		BusinessID:  "acme-corp",
		Capabilities: []agent.Capability{
			"doc-read",
			"doc-extract",
			"doc-validate",
		},
		Permissions: []agent.Authority{
			agent.AuthorityRead,
			agent.AuthorityExecute,
		},
		Authority:       agent.AuthorityExecute,
		ParentAuthority: agent.AuthorityAdmin,
		ToolIDs:         []string{"doc-read", "doc-extract", "doc-validate", "doc-store"},
		Budget: agent.Budget{
			MaxTokens: 100000,
			MaxCost:   10.0,
			MaxTasks:  1000,
		},
		SpawnLimits: agent.SpawnLimits{
			MaxChildren:    5,
			MaxDepth:       2,
			MaxDescendants: 25,
		},
	}

	ag, err := engine.AgentRuntime().ProvisionAgent(def)
	if err != nil {
		logger.Error("failed to provision agent", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
		return 1
	}
	logger.Info("agent provisioned", logging.Fields{
		Context: map[string]any{"agent_id": ag.ID, "name": def.Name},
	})

	// Start the agent
	if err := engine.AgentRuntime().StartAgent(ag.ID); err != nil {
		logger.Error("failed to start agent", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
		return 1
	}
	logger.Info("agent started", logging.Fields{
		Context: map[string]any{"agent_id": ag.ID},
	})

	// Create the HTTP gateway
	gw := gateway.NewServer(engine, addr)

	// Wire lifecycle
	life.RegisterHook(lifecycle.Hook{
		Name: "doc-processor",
		Init: func(ctx context.Context) error {
			return engine.Start(ctx)
		},
		Close: func(ctx context.Context) error {
			return engine.Stop(ctx)
		},
	})

	// Start lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startCtx, startCancel := context.WithTimeout(ctx, 10*time.Second)
	defer startCancel()

	if err := life.Start(startCtx); err != nil {
		logger.Error("startup failed", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
		return 1
	}

	// Start gateway server
	go func() {
		logger.Info("gateway starting", logging.Fields{
			Context: map[string]any{"addr": addr},
		})
		if err := gw.Start(ctx); err != nil && err != http.ErrServerClosed {
			logger.Error("gateway error", logging.Fields{
				Context: map[string]any{"err": err.Error()},
			})
		}
	}()

	healthSrv.MarkReady()
	logger.Info("doc-processor ready", logging.Fields{
		Context: map[string]any{
			"addr":     addr,
			"agent_id": ag.ID,
		},
	})

	fmt.Printf("doc-processor ready on %s (agent: %s)\n", addr, ag.ID)
	fmt.Println("Try: curl -X POST http://localhost:9090/api/v1/requests \\")
	fmt.Println("  -H 'Content-Type: application/json' \\")
	fmt.Println("  -d '{\"intent\":\"process invoice INV-001\",\"business_id\":\"acme-corp\",\"actor_id\":\"user-1\"}'")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", logging.Fields{
			Context: map[string]any{"signal": sig.String()},
		})
	case <-ctx.Done():
	}

	// Graceful shutdown
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()

	if err := gw.Stop(shutCtx); err != nil {
		logger.Error("gateway stop error", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
	}

	if err := engine.Stop(shutCtx); err != nil {
		logger.Error("engine stop error", logging.Fields{
			Context: map[string]any{"err": err.Error()},
		})
	}

	logger.Info("doc-processor stopped", logging.Fields{})
	return 0
}

// registerDocTools registers example document processing tools.
func registerDocTools(reg *tool.ToolRegistry, logger *logging.Logger) {
	tools := []*tool.ToolDefinition{
		{
			ID:          "doc-read",
			Name:        "Read Document",
			Version:     "1.0.0",
			Description: "Reads document content from storage",
			Category:    tool.ToolCategoryRead,
			RiskLevel:   tool.RiskLevelLow,
			ReadOnly:    true,
			Timeout:     30 * time.Second,
		},
		{
			ID:          "doc-extract",
			Name:        "Extract Data",
			Version:     "1.0.0",
			Description: "Extracts structured data from document content",
			Category:    tool.ToolCategoryCompute,
			RiskLevel:   tool.RiskLevelLow,
			ReadOnly:    false,
			Timeout:     60 * time.Second,
		},
		{
			ID:          "doc-validate",
			Name:        "Validate Data",
			Version:     "1.0.0",
			Description: "Validates extracted data against business rules",
			Category:    tool.ToolCategoryCompute,
			RiskLevel:   tool.RiskLevelLow,
			ReadOnly:    false,
			Timeout:     30 * time.Second,
		},
		{
			ID:          "doc-store",
			Name:        "Store Result",
			Version:     "1.0.0",
			Description: "Stores processed document result",
			Category:    tool.ToolCategoryWrite,
			RiskLevel:   tool.RiskLevelMedium,
			ReadOnly:    false,
			Timeout:     30 * time.Second,
		},
	}

	for _, t := range tools {
		if err := reg.RegisterTool(t); err != nil {
			logger.Error("failed to register tool", logging.Fields{
				Context: map[string]any{"tool_id": t.ID, "err": err.Error()},
			})
		} else {
			logger.Info("tool registered", logging.Fields{
				Context: map[string]any{"tool_id": t.ID, "name": t.Name},
			})
		}
	}

	fmt.Printf("Registered %d tools\n", reg.ToolCount())
}
