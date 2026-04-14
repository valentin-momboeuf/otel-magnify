# Changelog

Toutes les modifications notables sont documentées ici.

## Unreleased

### Features
- Add sample OTel Collector configs for OpAMP demo
- Rename Agents to Inventory, clickable stat cards with type filter
- Remove total agents stat card from dashboard
- Add Collectors/SDK stat cards, remove pie chart
- Improve agent type detection (collector vs SDK)
- Add Postgres service profile to docker-compose
- Add agent status chart to Dashboard
- Add route guards for authenticated pages
- Add webhook notifications for alerts
- Add config_drift and version_outdated alert rules
- Redesign frontend with Signal Deck aesthetic
- Add Helm chart for Kubernetes deployment
- Add Dockerfile and docker-compose for deployment
- Redesign frontend with industrial terminal aesthetic
- Add layout, dashboard page, and routing
- Add API client, WebSocket client, and Zustand store
- Scaffold React frontend with Vite
- Wire server entrypoint with all backend components
- Add alert engine with agent_down rule evaluation
- Implement config, alert, and auth REST handlers
- Add REST API router with agent handlers
- Add WebSocket hub for real-time frontend updates
- Add JWT auth with middleware
- Add OpAMP server with agent registration and config push
- Add alert and user store operations
- Add config and agent_config store operations
- Add agent store CRUD operations
- Add database layer with goose migrations
- Add shared data models
- Initialize Go backend module with config
- Centralized OTel agent management via OpAMP
- Agent inventory with real-time status
- Remote config push via YAML editor
- Alert engine for agent downtime detection
- Docker Compose and Helm deployment
- JWT authentication

### Bug Fixes
- Remove unused total variable
- Use spec.ingressClassName instead of deprecated annotation
- Pass seed admin env vars through docker-compose
- Address critical review findings (CORS, embed, auth, DoS, healthz, seed)

### Documentation
- Add public roadmap (Now/Next/Later)
- Add contributing guide with DCO
- Add security policy and vulnerability reporting
- Add pre-1.0 status banner and BSL 1.1 badge
- Replace Apache 2.0 with BSL 1.1
- Add CLAUDE.md project instructions
- Write comprehensive README
- Add implementation plan for otel-magnify
- Translate design spec to English
- Add OpAMP management platform design spec

### Refactoring
- Use reverse map for OpAMP connection tracking
