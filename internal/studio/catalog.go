package studio

import (
	"net/http"
	"strings"

	"github.com/mtfuller/agentworks/internal/spec"
)

type catalogResponse struct {
	Teams       []catalogTeam      `json:"teams"`
	Workspaces  []catalogWorkspace `json:"workspaces"`
	Harnesses   []string           `json:"harnesses"`
	Permissions []spec.Permission  `json:"permissions"`
}

type catalogTeam struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	DefaultAgent string         `json:"default_agent,omitempty"`
	Agents       []catalogAgent `json:"agents"`
}

type catalogAgent struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	MaxPermission spec.Permission `json:"max_permission"`
}

type catalogWorkspace struct {
	Name  string `json:"name"`
	Bound bool   `json:"bound"`
}

func serveCatalog(writer http.ResponseWriter, request *http.Request, root string, harnesses []string) {
	if strings.TrimSpace(root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "project catalog is unavailable"})
		return
	}
	project, err := spec.LoadProject(root)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	local, err := spec.LoadLocal(root)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := catalogResponse{
		Teams: []catalogTeam{}, Workspaces: []catalogWorkspace{},
		Harnesses: append([]string(nil), harnesses...),
		Permissions: []spec.Permission{
			spec.PermissionReadonly, spec.PermissionReadwrite,
			spec.PermissionCollaborate, spec.PermissionAutonomous,
		},
	}
	for _, teamName := range project.Teams {
		team, err := spec.LoadTeam(root, teamName)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		entry := catalogTeam{
			Name: teamName, Description: team.Description, DefaultAgent: team.DefaultAgent,
			Agents: []catalogAgent{},
		}
		for _, agentName := range team.Agents {
			agent, err := spec.LoadAgent(root, agentName)
			if err != nil {
				writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			entry.Agents = append(entry.Agents, catalogAgent{
				Name: agentName, Description: agent.Description, MaxPermission: agent.MaxPermission,
			})
		}
		result.Teams = append(result.Teams, entry)
	}
	for _, workspace := range project.Workspaces {
		_, bound := local.Workspaces[workspace]
		result.Workspaces = append(result.Workspaces, catalogWorkspace{Name: workspace, Bound: bound})
	}
	writeJSON(writer, http.StatusOK, result)
}
