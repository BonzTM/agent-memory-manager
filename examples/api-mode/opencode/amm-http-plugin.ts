/**
 * OpenCode AMM HTTP Plugin
 * Configures OpenCode to use AMM via HTTP API instead of local binary.
 */
export default {
  name: 'amm-http',
  version: '1.1.0',
  description: 'Connect to AMM via HTTP API',
  config: {
    apiUrl: process.env.AMM_API_URL || 'http://localhost:8080',
    apiKey: process.env.AMM_API_KEY || '',
    projectId: process.env.AMM_PROJECT_ID || 'default'
  },
  hooks: {
    async onSessionStart(ctx) {
      const response = await fetch(`${this.config.apiUrl}/v1/recall`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(this.config.apiKey ? { Authorization: `Bearer ${this.config.apiKey}` } : {})
        },
        body: JSON.stringify({
          query: `Ambient context for project ${this.config.projectId}`,
          opts: { mode: 'ambient', limit: 20 }
        })
      });
      const data = await response.json();
      ctx.setContext('amm_recall', data.data);
    },
    async onMessage(_ctx, message) {
      await fetch(`${this.config.apiUrl}/v1/events`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(this.config.apiKey ? { Authorization: `Bearer ${this.config.apiKey}` } : {})
        },
        body: JSON.stringify({
          kind: message.role === 'user' ? 'message_user' : 'message_assistant',
          source_system: 'opencode',
          content: message.content,
          project_id: this.config.projectId
        })
      });
    }
  }
};
