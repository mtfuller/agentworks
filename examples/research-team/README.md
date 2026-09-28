# Non-software research team

This format-2 dogfood project proves that AgentWorks workspaces are ordinary folders, not Git
repositories. Bind `library` to any notes or documents directory—even an empty folder with no
`.git` directory—then use Studio to run the researcher or readonly editor.

```sh
cp agentworks.local.yaml.example agentworks.local.yaml
# Edit the absolute path, then:
agentworks plan research
agentworks pack research
agentworks studio --experimental
```

The researcher and editor share two skills and separate private memory. The checked-in manual
route demonstrates a non-software `research.requested` event; a direct Studio manual run can
also select either agent and either supported harness.
