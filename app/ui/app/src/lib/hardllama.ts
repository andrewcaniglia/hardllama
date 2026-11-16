// Features HardLlama leaves out. The upstream code stays, so merging Ollama's
// changes stays simple. These match thirdPartyAppsEnabled in
// app/cmd/app/hardllama_features.go and OllamaAccountsEnabled in app/ui/ui.go.

// Connecting other companies' AI apps, such as ChatGPT and Claude Desktop.
export const THIRD_PARTY_APPS = false;

// Signing in to ollama.com, Ollama's paid cloud models and web search. Prompts
// sent to those leave the computer.
export const OLLAMA_ACCOUNTS = false;
