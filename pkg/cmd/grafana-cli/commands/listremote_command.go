package commands

import (
	"github.com/Seasheller/grafana/pkg/cmd/grafana-cli/logger"
	s "github.com/Seasheller/grafana/pkg/cmd/grafana-cli/services"
	"github.com/Seasheller/grafana/pkg/cmd/grafana-cli/utils"
)

func listremoteCommand(c utils.CommandLine) error {
	plugin, err := s.ListAllPlugins(c.RepoDirectory())

	if err != nil {
		return err
	}

	for _, i := range plugin.Plugins {
		pluginVersion := ""
		if len(i.Versions) > 0 {
			pluginVersion = i.Versions[0].Version
		}

		logger.Infof("id: %v version: %s\n", i.Id, pluginVersion)
	}

	return nil
}
