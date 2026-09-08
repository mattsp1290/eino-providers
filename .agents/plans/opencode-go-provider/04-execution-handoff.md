# Execution handoff

Planning only: no provider code, dependency edit, or live inference has been performed by this plan.

## Start and order

First implementation action: create a clean branch/worktree from current `origin/main`, inspect its applicable `AGENTS.md`, and run `bd prime`. Preserve the original checkout's unrelated changes listed in [00-overview.md](00-overview.md). The resolved auth pin is available; no owner acceptance or upstream release request is pending.

Use Beads for implementation tracking. Claim existing implementation issue `eino-providers-l8a` before coding and split it into work issues as needed; do not use this specification as a status tracker. All new `opencodego/` paths and symbols in work files are proposals under the existing repository root.

| Order | Work package and files | Result and verification |
| --- | --- | --- |
| 1 | WP1: `go.mod`, `go.sum`, `options.go`; new config/provider/chatmodel files in `opencodego/` | Pinned dependency and validated public boundary; config/registration tests. |
| 2 | WP2: new `opencodego/transport.go`, `observation.go`, `errors.go` and tests | Auth composition, safe errors, preserved status/retry policy and typed causes; transport/error fixtures. |
| 3 | WP3: new `opencodego/adapters.go`, `stream.go`, facade, usage observer and tests | Chat Completions and Messages generation, stream and tools; wire usage presence, race and URL fixtures. |
| 4 | WP4: new Responses files and tests | Native Responses generation/stream/tool loop and lifecycle; protocol/race fixtures. |
| 5 | WP5: integration/live tests, example, existing docs and dependency-check script | Full deterministic matrix, documented consumer workflow and CI gates. |

WP1–WP3 share constructors and must land as one coherent implementation increment if intermediate commits cannot compile or would advertise incomplete behavior. WP4 follows the fixed facade/error contract. WP5 fixture scaffolding and docs can be drafted alongside WP4 after the public API settles, but final integration is sequential. Do not expose Responses selection as successful until its adapter exists; if delivering separate PRs, reject unimplemented protocols clearly and label the interim scope.

## Stop/go gates

1. Before dependency changes, resolve the exact pin without `replace` and compare downloaded public APIs to the inspected contract.
2. Before adapter acceptance, prove exact native URLs and typed errors through the real SDK plus auth transport. A mock model is insufficient.
3. Before Responses acceptance, prove incremental tool arguments, two-turn replay, immediate terminal completion, and cancel+Close cleanup against a silent HTTP fixture.
4. Before publishing the feature, run WP5 checks on the CI toolchains and preserve opt-in dependency isolation. Do not waive a failing behavior gate because no active consumers exist.

A newly discovered missing upstream capability is owned by that repository's maintainer; file a concrete Beads blocker and update readiness before proceeding with dependent work. Do not silently add credential/session ownership here. No current blocker is known.

## Integration, rollout, and rollback

The user requires no backward compatibility and no feature flags. This plan needs no data/config migration and no consumer rollout coordination. Its scoped regression expectation is that existing backend tests continue to pass. New options and backend import are explicit adoption points.

Rollback removes the new backend import/selection in an adopting host or reverts the implementation commits and module dependency. Do not delete host keys or sessions, because this library never owns their storage. Preserve shared `Options` fields if subsequent unrelated work begins using them; re-evaluate that only if evidence exists at rollback time.

## Definition of done

The root factory and direct chat-model API work for all three selected protocols. Text, streaming, function tools, usage, session isolation, cancellation, native routing, and safe error behavior pass the specified fixtures. CI builds both supported Go toolchains. Example and API docs agree with code. No local replacement, credential data, root auth dependency leak, or unrelated user edit is committed. Default tests make no paid requests. Live validation status is stated accurately.

Run the repository's required completion workflow: update/close the implementation Beads issues, commit only related files, synchronize current remote work in the clean checkout, run `bd dolt push`, push the implementation branch, and verify its upstream state. Do not clear pre-existing user stashes. Prune stale remote refs only after safe inspection. Report commit and actual remote status.

Deferred work: model capability discovery, additional Responses modalities/built-in tools, a generic shared Responses engine, account workflows, and downstream application adoption. None is required for this provider's completion. File new Beads issues only when a concrete follow-up is discovered; these exclusions do not authorize further implementation.
