// Package internal is the root of the opencode-dashboard backend.
//
// Dependency graph (acyclic, enforced by Go imports):
//
//		model ← ports ← {db, runtime, hub, collector}
//
//	  - internal/model: frozen REST/SSE contract types (SessionRow, Snapshot)
//	    plus the pure status-derivation function. Imports nothing in-repo.
//	  - internal/ports: interfaces (DBReader, RuntimeSource, SnapshotSink)
//	    and env config. Imports model only.
//	  - internal/db, internal/runtime, internal/hub, internal/collector:
//	    the four parallel Wave-1 builders. Each may import model and ports;
//	    none may be imported by model or ports (no reverse imports).
package internal
