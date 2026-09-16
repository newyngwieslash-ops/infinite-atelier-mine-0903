package providers

import (
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// ToConfigDTO converts a domain config to its safe transport view. The DTO
// carries the secret reference only; it can never carry a secret value.
func ToConfigDTO(config provider.Config) ConfigDTO {
	dto := ConfigDTO{
		ID:            config.ID,
		Kind:          string(config.Kind),
		DisplayName:   config.DisplayName,
		BaseURL:       config.BaseURL,
		SecretRef:     config.SecretRef,
		LocalApproved: config.LocalApproved,
		Enabled:       config.Enabled,
		Revision:      config.Revision,
	}
	if !config.UpdatedAt.IsZero() {
		dto.UpdatedAt = config.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return dto
}

// ToConfigDTOs converts a slice of configs.
func ToConfigDTOs(configs []provider.Config) []ConfigDTO {
	dtos := make([]ConfigDTO, 0, len(configs))
	for _, config := range configs {
		dtos = append(dtos, ToConfigDTO(config))
	}
	return dtos
}
