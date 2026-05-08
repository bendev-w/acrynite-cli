package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	service "acrynite-cli/services"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
)

// initCmd registers this agent with the Acrynite platform.
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Register this agent with the Acrynite platform",
	Long: `Interactively collects your account and project details, then calls
POST /api/agents/register to create your user, project, and agent in one step.
The returned token is saved to ~/.acrynite/config.json — keep it safe.`,
	RunE: runInit,
}

type initAnswers struct {
	Name               string
	Email              string
	Password           string
	ProjectName        string
	ProjectDescription string
	AgentName          string
}

func runInit(cmd *cobra.Command, _ []string) error {
	fmt.Println("  Acrynite — Agent Registration")
	fmt.Println()

	var a initAnswers

	questions := []*survey.Question{
		{
			Name:     "Name",
			Prompt:   &survey.Input{Message: "Your full name:"},
			Validate: survey.Required,
		},
		{
			Name:   "Email",
			Prompt: &survey.Input{Message: "Email address:"},
			Validate: survey.ComposeValidators(
				survey.Required,
				func(val interface{}) error {
					s, _ := val.(string)
					for _, c := range s {
						if c == '@' {
							return nil
						}
					}
					return fmt.Errorf("please enter a valid email address")
				},
			),
		},
		{
			Name:     "Password",
			Prompt:   &survey.Password{Message: "Password (min 8 chars):"},
			Validate: survey.MinLength(8),
		},
		{
			Name:     "ProjectName",
			Prompt:   &survey.Input{Message: "Project name:"},
			Validate: survey.Required,
		},
		{
			Name:   "ProjectDescription",
			Prompt: &survey.Input{Message: "Project description (optional):"},
		},
		{
			Name:     "AgentName",
			Prompt:   &survey.Input{Message: "Agent name:", Default: "prod-agent"},
			Validate: survey.Required,
		},
	}

	if err := survey.Ask(questions, &a); err != nil {
		return fmt.Errorf("prompt cancelled: %w", err)
	}

	req := service.RegisterRequest{
		Name:               a.Name,
		Email:              a.Email,
		Password:           a.Password,
		ProjectName:        a.ProjectName,
		ProjectDescription: a.ProjectDescription,
		AgentName:          a.AgentName,
		AgentType:          "cli",
	}

	baseURL, _ := cmd.Flags().GetString("url")

	fmt.Println()
	fmt.Print("Registering agent\xe2\x80\xa6 ")

	resp, err := service.Register(baseURL, req)
	if err != nil {
		fmt.Println("\xe2\x9c\x97")
		return err
	}

	fmt.Println("\xe2\x9c\x93")
	fmt.Println()
	fmt.Printf("  %s\n\n", resp.Message)
	fmt.Printf("  User    : #%d  %s <%s>\n", resp.User.ID, resp.User.Name, resp.User.Email)
	fmt.Printf("  Project : #%d  %s  [%s]\n", resp.Project.ID, resp.Project.Name, resp.Project.Status)
	fmt.Printf("  Agent   : #%d  %s  (type: %s)\n", resp.Agent.ID, resp.Agent.Name, resp.Agent.Type)
	fmt.Println()
	fmt.Printf("  Token   : %s\n", resp.Token)
	fmt.Println()
	fmt.Println("  \xe2\x9a\xa0  Store this token now \xe2\x80\x94 it will not be shown again.")
	fmt.Println()

	if err := saveConfig(resp); err != nil {
		fmt.Printf("  Warning: could not save config: %v\n", err)
	} else {
		fmt.Println("  Config saved to ~/.acrynite/config.json")
	}

	return nil
}

// localConfig is the shape of ~/.acrynite/config.json.
type localConfig struct {
	Token       string `json:"token"`
	AgentID     int64  `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	ProjectID   int64  `json:"project_id"`
	ProjectName string `json:"project_name"`
	UserEmail   string `json:"user_email"`
}

func saveConfig(resp *service.RegisterResponse) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	dir := filepath.Join(home, ".acrynite")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create config directory: %w", err)
	}

	cfg := localConfig{
		Token:       resp.Token,
		AgentID:     resp.Agent.ID,
		AgentName:   resp.Agent.Name,
		ProjectID:   resp.Project.ID,
		ProjectName: resp.Project.Name,
		UserEmail:   resp.User.Email,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal config: %w", err)
	}

	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("could not write config: %w", err)
	}

	return nil
}

func init() {
	initCmd.Flags().String("url", "http://localhost:8000", "Acrynite API base URL")
}
