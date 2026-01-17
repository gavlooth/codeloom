package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/generative-ai-go/genai"
	"github.com/heefoo/codeloom/internal/config"
	"google.golang.org/api/option"
)

type GoogleProvider struct {
	client      *genai.Client
	model       string
	temperature float32
	maxTokens   int
}

func NewGoogleProvider(cfg config.LLMConfig) (*GoogleProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("google API key required (set GOOGLE_API_KEY or GEMINI_API_KEY)")
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.APIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Google AI client: %w", err)
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-1.5-flash" // Default model
	}

	return &GoogleProvider{
		client:      client,
		model:       model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
	}, nil
}

func (p *GoogleProvider) Name() string {
	return "google"
}

func (p *GoogleProvider) Generate(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	model := p.client.GenerativeModel(p.model)

	// Apply options
	options := &GenerateOptions{
		Temperature: p.temperature,
		MaxTokens:   p.maxTokens,
	}
	for _, opt := range opts {
		opt(options)
	}

	// Set generation config
	model.SetTemperature(options.Temperature)
	if options.MaxTokens > 0 {
		model.SetMaxOutputTokens(int32(options.MaxTokens))
	}

	// Convert messages to Gemini format
	cs := model.StartChat()
	var systemContent string

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			systemContent = msg.Content
		case RoleUser:
			content := msg.Content
			if systemContent != "" {
				content = systemContent + "\n\n" + content
				systemContent = ""
			}
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(content)},
				Role:  "user",
			})
		case RoleAssistant:
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(msg.Content)},
				Role:  "model",
			})
		}
	}

	// Get last user message for sending
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			lastUserMsg = messages[i].Content
			// Remove the last user message from history since we'll send it
			if len(cs.History) > 0 {
				cs.History = cs.History[:len(cs.History)-1]
			}
			break
		}
	}

	if lastUserMsg == "" {
		return "", fmt.Errorf("no user message found")
	}

	resp, err := cs.SendMessage(ctx, genai.Text(lastUserMsg))
	if err != nil {
		return "", fmt.Errorf("google generate error: %w", err)
	}

	// Extract text from response
	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no response candidates")
	}

	var result string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			result += string(text)
		}
	}

	return result, nil
}

func (p *GoogleProvider) GenerateWithTools(ctx context.Context, messages []Message, tools []Tool) (*ToolCallResponse, error) {
	model := p.client.GenerativeModel(p.model)

	// Set generation config
	model.SetTemperature(p.temperature)
	if p.maxTokens > 0 {
		model.SetMaxOutputTokens(int32(p.maxTokens))
	}

	// Convert tools to Gemini format
	var genaiTools []*genai.Tool
	for _, tool := range tools {
		// Convert parameters to OpenAPI schema
		schema := convertToGenaiSchema(tool.Parameters)

		genaiTools = append(genaiTools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  schema,
				},
			},
		})
	}
	model.Tools = genaiTools

	// Convert messages to Gemini format
	cs := model.StartChat()
	var systemContent string

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			systemContent = msg.Content
		case RoleUser:
			content := msg.Content
			if systemContent != "" {
				content = systemContent + "\n\n" + content
				systemContent = ""
			}
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(content)},
				Role:  "user",
			})
		case RoleAssistant:
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(msg.Content)},
				Role:  "model",
			})
		}
	}

	// Get last user message
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			lastUserMsg = messages[i].Content
			if len(cs.History) > 0 {
				cs.History = cs.History[:len(cs.History)-1]
			}
			break
		}
	}

	if lastUserMsg == "" {
		return nil, fmt.Errorf("no user message found")
	}

	resp, err := cs.SendMessage(ctx, genai.Text(lastUserMsg))
	if err != nil {
		return nil, fmt.Errorf("google generate error: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no response candidates")
	}

	response := &ToolCallResponse{}

	// Check for function calls
	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.FunctionCall:
			argsJSON, err := json.Marshal(v.Args)
			if err != nil {
				argsJSON = []byte("{}")
			}
			response.ToolCalls = append(response.ToolCalls, ToolCall{
				ID:        fmt.Sprintf("call_%s", v.Name),
				Name:      v.Name,
				Arguments: string(argsJSON),
			})
		case genai.Text:
			response.Content += string(v)
		}
	}

	return response, nil
}

func (p *GoogleProvider) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	model := p.client.GenerativeModel(p.model)

	// Set generation config
	model.SetTemperature(p.temperature)
	if p.maxTokens > 0 {
		model.SetMaxOutputTokens(int32(p.maxTokens))
	}

	// Convert messages to Gemini format
	cs := model.StartChat()
	var systemContent string

	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			systemContent = msg.Content
		case RoleUser:
			content := msg.Content
			if systemContent != "" {
				content = systemContent + "\n\n" + content
				systemContent = ""
			}
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(content)},
				Role:  "user",
			})
		case RoleAssistant:
			cs.History = append(cs.History, &genai.Content{
				Parts: []genai.Part{genai.Text(msg.Content)},
				Role:  "model",
			})
		}
	}

	// Get last user message
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			lastUserMsg = messages[i].Content
			if len(cs.History) > 0 {
				cs.History = cs.History[:len(cs.History)-1]
			}
			break
		}
	}

	if lastUserMsg == "" {
		return nil, fmt.Errorf("no user message found")
	}

	ch := make(chan string, 100)

	go func() {
		defer close(ch)

		iter := cs.SendMessageStream(ctx, genai.Text(lastUserMsg))
		for {
			select {
			case <-ctx.Done():
				return
			default:
				resp, err := iter.Next()
				if err != nil {
					log.Printf("google stream error: %v", err)
					return
				}

				for _, cand := range resp.Candidates {
					for _, part := range cand.Content.Parts {
						if text, ok := part.(genai.Text); ok {
							select {
							case ch <- string(text):
							case <-ctx.Done():
								return
							}
						}
					}
				}
			}
		}
	}()

	return ch, nil
}

// convertToGenaiSchema converts a map-based schema to genai.Schema
func convertToGenaiSchema(params map[string]interface{}) *genai.Schema {
	if params == nil {
		return nil
	}

	schema := &genai.Schema{
		Type: genai.TypeObject,
	}

	if props, ok := params["properties"].(map[string]interface{}); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for name, prop := range props {
			propMap, ok := prop.(map[string]interface{})
			if !ok {
				continue
			}
			propSchema := &genai.Schema{}

			if t, ok := propMap["type"].(string); ok {
				switch t {
				case "string":
					propSchema.Type = genai.TypeString
				case "integer":
					propSchema.Type = genai.TypeInteger
				case "number":
					propSchema.Type = genai.TypeNumber
				case "boolean":
					propSchema.Type = genai.TypeBoolean
				case "array":
					propSchema.Type = genai.TypeArray
				case "object":
					propSchema.Type = genai.TypeObject
				}
			}

			if desc, ok := propMap["description"].(string); ok {
				propSchema.Description = desc
			}

			schema.Properties[name] = propSchema
		}
	}

	if required, ok := params["required"].([]string); ok {
		schema.Required = required
	} else if required, ok := params["required"].([]interface{}); ok {
		for _, r := range required {
			if s, ok := r.(string); ok {
				schema.Required = append(schema.Required, s)
			}
		}
	}

	return schema
}

func (p *GoogleProvider) Close() error {
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
