package plan

import (
	"fmt"
	"strings"
)

// FormatDiff formats the plan as a human-readable diff
func FormatDiff(plan *Plan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Stack: %s\n", plan.StackName))
	sb.WriteString(strings.Repeat("=", 60) + "\n\n")

	hasChanges := false

	// Networks
	if len(plan.Networks) > 0 {
		networksWithChanges := false
		for _, net := range plan.Networks {
			if net.Action != ActionNone {
				networksWithChanges = true
				break
			}
		}

		if networksWithChanges {
			sb.WriteString("Networks:\n")
			for _, net := range plan.Networks {
				if net.Action != ActionNone {
					sb.WriteString(fmt.Sprintf("  %s %s\n", actionSymbol(net.Action), net.Name))
					hasChanges = true
				}
			}
			sb.WriteString("\n")
		}
	}

	// Volumes
	if len(plan.Volumes) > 0 {
		volumesWithChanges := false
		for _, vol := range plan.Volumes {
			if vol.Action != ActionNone {
				volumesWithChanges = true
				break
			}
		}

		if volumesWithChanges {
			sb.WriteString("Volumes:\n")
			for _, vol := range plan.Volumes {
				if vol.Action != ActionNone {
					sb.WriteString(fmt.Sprintf("  %s %s\n", actionSymbol(vol.Action), vol.Name))
					hasChanges = true
				}
			}
			sb.WriteString("\n")
		}
	}

	// Configs
	if len(plan.Configs) > 0 {
		configsWithChanges := false
		for _, cfg := range plan.Configs {
			if cfg.Action != ActionNone {
				configsWithChanges = true
				break
			}
		}

		if configsWithChanges {
			sb.WriteString("Configs:\n")
			for _, cfg := range plan.Configs {
				if cfg.Action != ActionNone {
					sb.WriteString(fmt.Sprintf("  %s %s\n", actionSymbol(cfg.Action), cfg.Name))
					hasChanges = true
				}
			}
			sb.WriteString("\n")
		}
	}

	// Secrets
	if len(plan.Secrets) > 0 {
		secretsWithChanges := false
		for _, sec := range plan.Secrets {
			if sec.Action != ActionNone {
				secretsWithChanges = true
				break
			}
		}

		if secretsWithChanges {
			sb.WriteString("Secrets:\n")
			for _, sec := range plan.Secrets {
				if sec.Action != ActionNone {
					sb.WriteString(fmt.Sprintf("  %s %s\n", actionSymbol(sec.Action), sec.Name))
					hasChanges = true
				}
			}
			sb.WriteString("\n")
		}
	}

	// Services
	if len(plan.Services) > 0 {
		servicesWithChanges := false
		for _, svc := range plan.Services {
			if svc.Action != ActionNone {
				servicesWithChanges = true
				break
			}
		}

		if servicesWithChanges {
			sb.WriteString("Services:\n")
			for _, svc := range plan.Services {
				if svc.Action != ActionNone {
					sb.WriteString(fmt.Sprintf("  %s %s", actionSymbol(svc.Action), svc.Name))
					if len(svc.Changes) > 0 && svc.Action == ActionUpdate {
						sb.WriteString(fmt.Sprintf(" (changes: %s)", strings.Join(svc.Changes, ", ")))
					}
					sb.WriteString("\n")
					hasChanges = true
				}
			}
			sb.WriteString("\n")
		}
	}

	// Orphaned resources
	if len(plan.OrphanedServices) > 0 || len(plan.OrphanedNetworks) > 0 ||
		len(plan.OrphanedVolumes) > 0 || len(plan.OrphanedConfigs) > 0 ||
		len(plan.OrphanedSecrets) > 0 {
		sb.WriteString("Orphaned resources (use --prune to remove):\n")

		if len(plan.OrphanedServices) > 0 {
			sb.WriteString("  Services: " + strings.Join(plan.OrphanedServices, ", ") + "\n")
		}
		if len(plan.OrphanedNetworks) > 0 {
			sb.WriteString("  Networks: " + strings.Join(plan.OrphanedNetworks, ", ") + "\n")
		}
		if len(plan.OrphanedVolumes) > 0 {
			sb.WriteString("  Volumes: " + strings.Join(plan.OrphanedVolumes, ", ") + "\n")
		}
		if len(plan.OrphanedConfigs) > 0 {
			sb.WriteString("  Configs: " + strings.Join(plan.OrphanedConfigs, ", ") + "\n")
		}
		if len(plan.OrphanedSecrets) > 0 {
			sb.WriteString("  Secrets: " + strings.Join(plan.OrphanedSecrets, ", ") + "\n")
		}
		sb.WriteString("\n")
	}

	if !hasChanges {
		sb.WriteString("No changes detected.\n")
	}

	return sb.String()
}

// actionSymbol returns a visual symbol for the action type
func actionSymbol(action ActionType) string {
	switch action {
	case ActionCreate:
		return "+ create"
	case ActionUpdate:
		return "~ update"
	case ActionDelete:
		return "- delete"
	case ActionNone:
		return "  (no change)"
	default:
		return "?"
	}
}
