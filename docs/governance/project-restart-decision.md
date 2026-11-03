# Project Restart Decisions

Date: 2026-11-03
Authority: project owner, this conversation

The V1.11 requirements document is a reference for product behavior and terminology. It is not a frozen acceptance baseline for the restarted project. Current project decisions and GSD phase acceptance criteria govern development.

The eleven historical records and scripts listed by V1.11 were cleared during the project restart. The owner confirmed they are lost and directed that they not be recreated. Keep their evidence-register rows marked `missing` for factual accuracy; NFR-038 historical-record recovery is out of scope and is not a development, Phase 1, or release gate.

PostgreSQL and Valkey are external data services used during development and deployment. Their service images are not VORTEX product dependencies or distributed VORTEX artifacts. Record exact service versions, provenance, and image digests, but exclude their image operating-system package license closure from the VORTEX product dependency license gate. Continue license review for VORTEX code, linked dependencies, and artifacts distributed as part of the product.

Engineering checks are applied when a phase is ready to use the corresponding tool or dependency. M0 may proceed on its approved Go toolchain and standard-library-only Engine boundary while later-phase dependency closure continues before those dependencies are introduced.
