package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/heefoo/codeloom/internal/llm"
)

const MaxIterations = 10

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Execute     func(ctx context.Context, args map[string]interface{}) (string, error)
}

type AgentOutput struct {
	Answer       string `json:"answer"`
	Findings     string `json:"findings"`
	StepsTaken   int    `json:"steps_taken"`
	ToolUseCount int    `json:"tool_use_count"`
	Confidence   string `json:"confidence"`
}

type Agent struct {
	llm       llm.Provider
	tools     map[string]Tool
	maxIter   int
	systemMsg string
}

type AgentConfig struct {
	LLM       llm.Provider
	MaxIter   int
	SystemMsg string
}

func NewAgent(cfg AgentConfig) *Agent {
	maxIter := cfg.MaxIter
	if maxIter == 0 {
		maxIter = MaxIterations
	}

	systemMsg := cfg.SystemMsg
	if systemMsg == "" {
		systemMsg = defaultSystemMessage
	}

	return &Agent{
		llm:       cfg.LLM,
		tools:     make(map[string]Tool),
		maxIter:   maxIter,
		systemMsg: systemMsg,
	}
}

func (a *Agent) RegisterTool(tool Tool) {
	a.tools[tool.Name] = tool
}

func (a *Agent) RegisterTools(tools []Tool) {
	for _, tool := range tools {
		a.RegisterTool(tool)
	}
}

// Execute runs the ReAct loop: Think → Act → Observe → Repeat
func (a *Agent) Execute(ctx context.Context, query string) (*AgentOutput, error) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: a.systemMsg},
		{Role: llm.RoleUser, Content: a.buildInitialPrompt(query)},
	}

	output := &AgentOutput{
		Confidence: "medium",
	}

	for i := 0; i < a.maxIter; i++ {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		output.StepsTaken = i + 1

		// Get LLM response with tools
		llmTools := a.buildLLMTools()
		response, err := a.llm.GenerateWithTools(ctx, messages, llmTools)
		if err != nil {
			return nil, fmt.Errorf("llm error at step %d: %w", i+1, err)
		}

		// Check if LLM wants to use a tool
		if len(response.ToolCalls) > 0 {
			for _, tc := range response.ToolCalls {
				output.ToolUseCount++

				// Execute the tool
				toolResult, err := a.executeTool(ctx, tc)
				if err != nil {
					toolResult = fmt.Sprintf("Error: %v", err)
				}

				// Add assistant message with tool call
				messages = append(messages, llm.Message{
					Role:    llm.RoleAssistant,
					Content: response.Content,
				})

				// Add tool result
				messages = append(messages, llm.Message{
					Role:       llm.RoleTool,
					Content:    toolResult,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
			continue
		}

		// No tool calls - check if we have a final answer
		if response.Content != "" {
			// Try to parse as structured output
			if parsed, ok := a.parseStructuredOutput(response.Content); ok {
				output.Answer = parsed.Answer
				output.Findings = parsed.Findings
				if parsed.Confidence != "" {
					output.Confidence = parsed.Confidence
				}
			} else {
				output.Answer = response.Content
				output.Findings = "Analysis complete"
			}
			return output, nil
		}
	}

	// Max iterations reached
	output.Answer = "Analysis incomplete - maximum iterations reached"
	output.Confidence = "low"
	return output, nil
}

func (a *Agent) buildInitialPrompt(query string) string {
	toolDescriptions := a.buildToolDescriptions()

	return fmt.Sprintf(`You are a code analysis agent. Your task is to analyze the following query and provide a comprehensive answer.

## Query
%s

## Available Tools
%s

## Instructions
1. Think about what information you need to answer the query
2. Use the available tools to gather information
3. After gathering enough information, provide your final answer

## Output Format
When you have enough information, provide your answer in this JSON format:
{
  "answer": "Your comprehensive answer",
  "findings": "Summary of key findings",
  "confidence": "high/medium/low"
}

Begin your analysis.`, query, toolDescriptions)
}

func (a *Agent) buildToolDescriptions() string {
	var descriptions []string
	for name, tool := range a.tools {
		descriptions = append(descriptions, fmt.Sprintf("- %s: %s", name, tool.Description))
	}
	return strings.Join(descriptions, "\n")
}

func (a *Agent) buildLLMTools() []llm.Tool {
	var tools []llm.Tool
	for _, tool := range a.tools {
		tools = append(tools, llm.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return tools
}

func (a *Agent) executeTool(ctx context.Context, tc llm.ToolCall) (string, error) {
	tool, ok := a.tools[tc.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", tc.Name)
	}

	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	return tool.Execute(ctx, args)
}

type structuredOutput struct {
	Answer     string `json:"answer"`
	Findings   string `json:"findings"`
	Confidence string `json:"confidence"`
}

func (a *Agent) parseStructuredOutput(content string) (*structuredOutput, bool) {
	// Try to find JSON in the content
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || start >= end {
		return nil, false
	}

	jsonStr := content[start : end+1]
	var output structuredOutput
	if err := json.Unmarshal([]byte(jsonStr), &output); err != nil {
		return nil, false
	}

	if output.Answer == "" {
		return nil, false
	}

	return &output, true
}

const defaultSystemMessage = `You are a code analysis agent with access to tools for searching and analyzing code.
Your goal is to help users understand codebases by providing accurate, well-researched answers.

Guidelines:
- Use tools to gather information before making conclusions
- Be precise about file locations and line numbers
- Acknowledge uncertainty when information is incomplete
- Provide actionable insights when possible`
