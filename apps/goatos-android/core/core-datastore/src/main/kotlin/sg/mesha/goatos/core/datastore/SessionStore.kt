package sg.mesha.goatos.core.datastore

// Persistence port for session/auth state. Real impl (DataStore-backed) lands in
// a later pass; the fake keeps the graph wired for now.

interface SessionStore

class FakeSessionStore : SessionStore
