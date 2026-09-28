package spec

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func (command *Command) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return fmt.Errorf("command must be a string or a sequence of strings")
		}
		*command = Command{node.Value}
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return fmt.Errorf("command must be a string or a sequence of strings: %w", err)
		}
		*command = Command(values)
		return nil
	default:
		return fmt.Errorf("command must be a string or a sequence of strings")
	}
}

func (command Command) MarshalYAML() (any, error) {
	if len(command) == 1 {
		return command[0], nil
	}
	return []string(command), nil
}
