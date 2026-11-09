# Independent committed-known-usage inventory

Candidate child final now includes an explicit version1 count/digest over sorted
batch IDs and SHA256 of original committed wire payloads. It excludes returned
local tickets, retains the local ticket digest and does not release PG funds.
Empty inventory has a domain-separated digest. Version0 historical immutable
finals replay unchanged and cannot validate as complete known inventory.

Contracts test ordering, duplicate rejection, original payload mutation and
missing inventory. Store closure test independently reconstructs a committed
batch while excluding a returned ticket, then verifies immutable reopen replay.
Historical final regression proves no silent upgrade. Full native root
Agent/Store/Hub/contracts/Node CLI race passed, as did Linux arm64 component race
and Node linux amd64+arm64 builds. These are development results, not clean-source
or Actions acceptance. Complete PG consumer/parent release, sustained funding,
explicit recovery and CR-008 remain pending. Phase6 stays8/11.
