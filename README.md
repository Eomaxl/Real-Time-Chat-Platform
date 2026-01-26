# Real-Time-Chat-Platform + WebRTC Signaling + Minimal SFU 

## 1) Product requirement
## 1.1 Vision

Build a production-grade real-time communication platform that supports:
- Chat (channels + DMs), typing, read receipts, message history
- Presence (online/offline/last seen)
- Calls (audio/video) via WebRTC, with a reliable signaling layer
- Optional: a Minimal SFU to scale calls beyond P2P
This mirrors Stream’s core domains: chat API + edge-friendly real-time infrastructure + (optionally) video SFU.

## 1.2 Target Users & Personas

### 1. End User
- Wants fast chat + reliable calls
- Expects instant delivery, message history, stable reconnects
### 2. App Developer (your “customer”)
- Integrates via API + SDK
- Wants stable public APIs, docs, versioning, predictable behavior
### 3. Operator (SRE mindset)
- Needs observability, rate limiting, abuse prevention, rollouts

## 1.3 Core Use Cases
### Chat
- Create channel, invite users, send messages
- Real-time updates via WebSocket
- Fetch history with pagination
- Typing indicators
- Read receipts
### Presence
- User online/offline (heartbeat)
- “last active”
- presence updates to channel members
### Calls (WebRTC)
- Start call in a channel/DM
- Join/leave
- WebRTC signaling: offer/answer + ICE candidates
- Handle reconnect, renegotiation, ICE restart
- Optional: escalate to SFU for group calls

## 1.4 Product Scope
### MVP (must-have)
### Chat
- DM + Channel messaging
- Message history (persisted)
- WebSocket events for real-time message delivery
- Idempotent message send (no duplicates)
### Presence
- Heartbeats
- Online/offline status
- Presence change events
### WebRTC Signaling
- Call sessions (create/join/leave)
- Offer/Answer exchange
- Trickle ICE candidates
- Basic permissions (only members can join)
### V1 (strong for interviews)
- Read receipts
- Typing events (debounced)
- Offline message delivery semantics (history fetch + last seen offsets)
- Rate limiting & abuse prevention
- SDKs (Go + JS)
### Optional (differentiator)
### Minimal SFU
- 1 room, N participants
- Forward tracks to all others (no mixing)
- Basic subscription management
- Basic bandwidth considerations documented



## 2) Technical Requirements
## 2.1 System Architecture (services)
### Required services (Go)
### API Gateway / Edge
- REST endpoints + WebSocket endpoint
- Auth, rate limiting, request validation
- Routes to internal services

### Chat Service
- Persist messages to Postgres
- Publish real-time events (to WS hub via Redis PubSub/NATS)
- Read receipts + typing events (optional)

### Presence Service
- Heartbeat ingestion
- Presence state stored in Redis with TTL
- Presence change events

### Call/Signaling Service
- Manages call sessions (DB)
- Signaling message relay (WS → target participants)
- Enforces signaling state machine per participant

### Optional service
### SFU Service
- Pion WebRTC based
- Receives publisher tracks
- Forwards to subscribers

## 2.2 Protocols
- REST (JSON) for CRUD operations
- WebSocket for real-time events:
  - chat events
  - presence events
  - signaling events
- Optional: gRPC internal service communication (not required, but nice)

## 2.3 Authentication & Authorization
### Auth
- JWT access token (or opaque token with introspection)
- Token includes user_id, tenant_id (optional), roles

### Authorization rules
- Only members can:
- read/write channel messages
-receive channel events
- join call sessions for that channel
- Server validates membership on:
    - REST calls
    - WS subscriptions
    - signaling messages

## 4) Engineering Milestones (practical build order)
- Chat REST + Postgres + idempotency
- WebSocket hub + channel subscriptions + message events
- Presence (Redis TTL + WS updates)
- Call sessions (DB + join/leave events)
- WebRTC signaling (offer/answer/ICE + state machine)
- SDKs (Go + JS)
- Load tests + observability
- Optional SFU

## Infrastructure
- Postgres / CockroachDB → persistence
- Redis → presence, rate limiting, pub/sub
- WebSockets → real-time events
- Docker / Kubernetes → deployment

## SDKs
- Go SDK
- JavaScript SDK

### SDKs expose:
- Chat APIs
- Presence
- Call lifecycle
- WebSocket subscriptions
- Public API design & versioning are first-class concerns.