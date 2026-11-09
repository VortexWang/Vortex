# Phase4/Phase6 integration development evidence

These results do not constitute the final same-SHA Docker/Actions qualification.
Original compile/fixture failures, boundary RED and first full merged root/backend FAIL are retained. The early recovering-green file is a failed intermediate owner fixture and is NOT a passing result. The final recovering-all-green covers all five original scenarios.

Corrected complete native root race passed (Agent178.021s, Store147.036s, Phase5 process suite86.087s), as did complete native backend race with real PostgreSQL/Valkey (service569.990s). They ran in the development working tree and did not enable the real full-disk fixture. See validation-manifest.json for log hashes and scope.

Two independent Protobuf generations matched committed output. Six Linux cross-builds (Backend/Hub/Node, amd64/arm64) report exact fb3a97860e47bdee342ca3ef2e8cab19ecc7118d and vcs.modified=false. Cross-builds do not establish execution on either architecture. Two independent sqlc generations matched all33 committed outputs; the dependency/license and27 contract/evidence checks passed.

Clean fb3a9786 Linux arm64 root/Engine race passed with the real4MiB full-disk fixture (Agent253.534s, Store258.953s, Phase5 process suite112.495s). Isolated real-PG/Valkey Linux backend race also passed (service550.929s). Source remained clean before/after both runs. The archived environment records the pinned image, virtualized arm64 host, two-CPU limit and exact commands. Final freeze gates require exact CR-012 owner approval; final Docker/Actions pairing and main integration remain pending.
