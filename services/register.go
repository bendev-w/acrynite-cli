package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ─── Request ──────────────────────────────────────────────────────────────────

// RegisterRequest is the payload sent to POST /api/agents/register.
type RegisterRequest struct {
	Name               string `json:"name"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	ProjectName        string `json:"project_name"`
	ProjectDescription string `json:"project_description,omitempty"`
	AgentName          string `json:"agent_name"`
	AgentType          string `json:"agent_type"`
}

// ─── Response ─────────────────────────────────────────────────────────────────

type RegisterUserPayload struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type RegisterProjectPayload struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type RegisterAgentPayload struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// RegisterResponse is the successful response from POST /api/agents/register.
type RegisterResponse struct {
	Message string                 `json:"message"`
	User    RegisterUserPayload    `json:"user"`
	Project RegisterProjectPayload `json:"project"`
	Agent   RegisterAgentPayload   `json:"agent"`
	Token   string                 `json:"token"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

// Register calls POST /api/agents/register and returns the parsed response.
func Register(baseURL string, req RegisterRequest) (*RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}

	httpResp, err := client.Post(
		baseURL+"/api/agents/register",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusCreated && httpResp.StatusCode != http.StatusOK {
		var apiErr struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.NewDecoder(httpResp.Body).Decode(&apiErr)
		msg := apiErr.Message
		if msg == "" {
			msg = apiErr.Error
		}
		if msg == "" {
			msg = httpResp.Status
		}
		return nil, fmt.Errorf("server error (%d): %s", httpResp.StatusCode, msg)
	}

	var resp RegisterResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}
