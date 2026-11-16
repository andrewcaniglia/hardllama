package main

// HardLlama leaves out Ollama's connections to other companies' AI apps:
// the Claude Desktop gateway, the ChatGPT (Codex) desktop integration, and
// their installers. Turning this on brings back upstream's behavior; the
// code is kept so that merging upstream changes stays simple.
const thirdPartyAppsEnabled = false
