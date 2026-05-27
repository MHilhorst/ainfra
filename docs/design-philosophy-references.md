# Design Philosophy References: npm and Terraform

This document captures the guiding design philosophies of two tools worth studying as reference points for `ainfra`. They are not similar tools, but they share a common structural DNA: both use a **manifest at the repo root** that declares intent, a **lockfile** that pins exact state, an **extension ecosystem** built around a public registry, and a **CLI that separates declaration from execution**. One manages declarative dependencies (npm), the other manages declarative infrastructure state (Terraform). Together they bracket the design space that `ainfra` occupies.

---

## Part 1: npm

### 1. Origin and Problem Framing

Isaac Schlueter created npm in September 2009 after joining Node.js early. His framing was pragmatic: he was accustomed to package managers at Yahoo and "just sort of packed together this thing that was basically what I was doing by hand." ([Increment interview](https://increment.com/development/interview-with-isaac-z-schlueter-ceo-of-npm/))

The deliberate philosophical stance was **Unix philosophy applied to JavaScript**. Schlueter's adaptation, published in 2013, restated Doug McIlroy's original rule: "Write modules that do one thing and do it well. Write a new module rather than complicate an old one." ([blog.izs.me](https://blog.izs.me/2013/04/unix-philosophy-and-nodejs/)) The small-module ecosystem was intentional from the start — not an accident of community behavior.

npm's stated core goal is simple: act as "the world's largest software registry" so developers can "share and borrow packages" without geographic or logistical friction. ([docs.npmjs.com/about-npm](https://docs.npmjs.com/about-npm))

### 2. Core Design Principles

**Small, composable modules over monoliths.** Schlueter's hierarchy of values: "Working is better than perfect. Focus is better than features. Compatibility is better than purity. Simplicity is better than anything." These read as anti-feature-creep rules for every package on the registry, not just for npm itself. ([blog.izs.me](https://blog.izs.me/2013/04/unix-philosophy-and-nodejs/))

**Flexibility over opinion.** "We strategically built a tool that was easy to use and flexible." The acknowledged tradeoff: "the more flexible a tool is, the less opinionated it is." This choice enabled novel uses npm never anticipated but created ecosystem fragmentation (many near-identical packages). ([Increment interview](https://increment.com/development/interview-with-isaac-z-schlueter-ceo-of-npm/))

**Semantic versioning as a social contract.** npm adopted SemVer as the versioning standard: `major.minor.patch`, where major signals breaking changes, minor signals new backwards-compatible features, and patch signals bug fixes. The caret (`^`) became the default save prefix in February 2014, allowing minor and patch updates but blocking major bumps. The tilde (`~`) permits only patch updates — stricter, suited for stability-sensitive projects. ([nodesource.com/blog/semver-tilde-and-caret](https://nodesource.com/blog/semver-tilde-and-caret))

**Streams as the universal interface.** One of the Unix-derived rules: "Write modules that handle data Streams, because that is the universal interface." This echoes the pipeline composition model — modules should be agnostic about input source and output destination. ([blog.izs.me](https://blog.izs.me/2013/04/unix-philosophy-and-nodejs/))

### 3. CLI Ergonomics

npm uses a `<tool> <verb>` structure consistently: `npm install`, `npm run`, `npm init`, `npm publish`, `npm update`. Verbs are plain English imperatives. The pattern is flat and predictable — there is no deep sub-command nesting.

The `npm init` command bootstraps a project by generating `package.json` through a questionnaire. This "guided scaffolding at init" pattern sets up the manifest before anything else happens, establishing the single source of truth.

`npm install` is idempotent: running it multiple times produces the same result. Running it with no arguments restores the full dependency tree from the manifest and lockfile, making it a reliable "get me to declared state" command.

Human-vs-machine output is not a first-class npm concern — the CLI outputs for humans by default. The `--json` flag exists on some commands (e.g., `npm outdated --json`) but is not consistently applied across the surface.

### 4. Configuration Model

The **`package.json`** file at the repo root is the single manifest. It declares:
- Package identity (`name`, `version`, `description`)
- Runtime dependencies (`dependencies`) with semver ranges
- Development-only dependencies (`devDependencies`)
- Peer requirements (`peerDependencies`)
- Scripts (`scripts`) — arbitrary shell commands invoked via `npm run <script>`
- Workspace paths (`workspaces`) for monorepo layouts

The manifest intentionally uses **ranges** rather than exact versions. This allows patch/minor upgrades to flow automatically and reflects the "small modules iterate quickly" culture. Exact pinning is delegated to the lockfile.

**npm workspaces** (introduced in npm 7, 2020) solve monorepo workflows. Declaring `"workspaces": ["packages/*"]` in the root `package.json` causes `npm install` to symlink local packages into `node_modules`, removing the need for manual `npm link` calls. Workspace packages use standard version range syntax so they remain portable to the registry if promoted. ([docs.npmjs.com/workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/))

**Scoped packages** (`@org/package-name`) prevent naming collisions and enable per-scope registry routing via `.npmrc`. This is how private registries are pointed to without changing global config. ([docs.npmjs.com/scope](https://docs.npmjs.com/cli/v11/using-npm/scope/))

### 5. State and Reproducibility

The **`package-lock.json`** lockfile is npm's answer to "what is actually installed." It records the exact resolved version, download location, and integrity hash for every package in the tree. The official guarantee: "teammates, deployments, and continuous integration are guaranteed to install exactly the same dependencies." ([docs.npmjs.com/package-lock-json](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/))

The conceptual model: `package.json` is the *intent* (ranges); `package-lock.json` is the *resolved fact* (exact tree). npm install is a pure function that should map the same lockfile to the same `node_modules` tree every time.

From npm v7, the lockfile is self-contained enough that npm no longer needs to read `package.json` files for most operations — the lockfile carries all necessary metadata for performance optimization.

The lockfile must be committed to source control. It cannot be published to the registry (unlike `npm-shrinkwrap.json`, which is the publishable equivalent for tools distributed as packages).

### 6. Extensibility

npm's extensibility model is the registry itself. Any package can depend on any other package. The extension surface is intentionally unbounded: there is no plugin API for the CLI — instead, you install CLIs as packages (`eslint`, `prettier`, `vite`), and invoke them via `npm run` scripts or `npx`.

Scoped registries and private registries (Artifactory, GitHub Packages, Verdaccio) allow organizations to host internal packages without going through the public registry. The `.npmrc` file routes scopes to registries, enabling mixed public/private dependency trees with no changes to package.json.

### 7. Notable Tradeoffs and Criticisms

**`node_modules` size and phantom dependencies.** npm's flat dependency tree (introduced in npm 3 to solve Windows path-length limits) caused a new problem: phantom dependencies — packages you can `require()` but did not explicitly declare, because they were hoisted by the flattening algorithm. This makes dependency trees implicit and fragile. pnpm responded by using content-addressable storage and symlinks to enforce strict dependency isolation, 2-3x faster installs, and 50-70% less disk space. ([dev.to/leoat12/the-nodemodules-problem](https://dev.to/leoat12/the-nodemodules-problem-29dc))

**Semver as social contract, not technical guarantee.** The caret range assumes maintainers follow SemVer correctly. In practice, breaking changes ship in minor versions regularly. The lockfile papers over this in local development, but fresh installs on new machines or CI environments without a lockfile can break. The lesson: lockfiles are load-bearing, not optional.

**left-pad incident (2016).** A single 11-line module being unpublished broke thousands of builds globally, exposing the fragility of deep, fine-grained dependency trees. npm responded with unpublish policies and package deprecation controls, but the incident revealed that "small modules" as a philosophy creates systemic supply-chain risk.

---

## Part 2: Terraform

### 1. Origin and Problem Framing

Terraform was created by Mitchell Hashimoto at HashiCorp and first released in 2014. The problem it addressed: infrastructure provisioning was manual, undocumented, and impossible to reproduce across environments. Teams used different tools for different clouds, accumulated "snowflake servers," and had no way to preview changes before making them.

The philosophical stance is captured in the **Tao of HashiCorp**, HashiCorp's eight founding principles that govern all their products. ([hashicorp.com/tao-of-hashicorp](https://www.hashicorp.com/en/tao-of-hashicorp)) The two most load-bearing for Terraform:

- **Versioning through Codification**: "all processes should be written as code, stored, and versioned." Code is the documentation. Code is the audit trail. Code is the runbook.
- **Automation through Codification**: codified knowledge becomes machine-executable, enabling operators to scale without growing headcount.

### 2. Core Design Principles

**Declarative, not imperative.** "Terraform configuration files are declarative, meaning that they describe the end state of your infrastructure." You specify *what* you want; Terraform determines the ordering and API calls needed to achieve it. ([developer.hashicorp.com/terraform/intro](https://developer.hashicorp.com/terraform/intro)) This is the sharpest design choice in Terraform — it requires a complete model of "current state" to compute diffs, which is why state exists.

**Workflows over technologies.** The Tao states: "focus on the end goal and workflow, rather than the underlying technologies." Provisioning a server is the same workflow whether the server is a bare-metal host, a VM, or a container. Terraform encodes that workflow once and delegates the technology-specific implementation to providers.

**Immutability.** "Immutability is the inability to be changed." The preferred pattern is to create a new resource version and destroy the old one rather than mutating in place. This produces clearer audit trails, predictable rollbacks, and eliminates configuration drift caused by partial upgrades.

**Simple, modular, composable.** Echoing Unix philosophy: "many smaller components with well-defined purposes that work together... functional on their own but can be combined with other components in new and innovative ways." ([hashicorp.com/tao-of-hashicorp](https://www.hashicorp.com/en/tao-of-hashicorp))

**Pragmatism.** "There are many situations in which the practical solution requires reevaluating our ideals." The Tao explicitly allows breaking from pure declarative patterns when the practical situation demands it — a deliberate escape valve against dogmatism.

### 3. CLI Ergonomics

Terraform's verb set is small and deliberate:

- `terraform init` — download providers and modules, initialize the working directory
- `terraform plan` — compute and display a diff between declared state and actual state
- `terraform apply` — execute the plan (prompts for confirmation by default)
- `terraform destroy` — apply a plan that removes all managed resources
- `terraform import` — bring existing resources under Terraform management

The **plan/apply separation** is the most influential ergonomic decision. Plan is read-only and safe to run repeatedly; apply is the commit. This maps directly to "show me what will change before I commit" — a human review gate baked into the CLI flow. The official framing: "safe, predictable, and reproducible creating or changing of infrastructure." ([developer.hashicorp.com/terraform/intro/core-workflow](https://developer.hashicorp.com/terraform/intro/core-workflow))

`terraform init` is a required bootstrap step that downloads providers and modules declared in configuration. This front-loads dependency resolution before any planning or applying, and produces the lockfile as a side effect.

Terraform outputs are human-readable by default, with color-coded `+` (add), `-` (destroy), and `~` (modify) indicators in plan output. Machine-readable output is available via `terraform plan -out=plan.bin` and `terraform show -json plan.bin`.

### 4. Configuration Model

**HCL (HashiCorp Configuration Language)** is the native config format, designed by Mitchell Hashimoto to fill a gap between generic serialization formats (JSON is too verbose, YAML is ambiguous) and full programming languages (too much power for config). The README describes HCL as "a syntax and API specifically designed for building structured configuration formats." ([github.com/hashicorp/hcl](https://github.com/hashicorp/hcl))

HCL is human-writable and machine-parseable. It supports JSON as a first-class equivalent — HCL and JSON configs can be mixed in the same directory. This enables human-authored `.tf` files alongside machine-generated `.tf.json` files.

The configuration model is **directory-based**: all `.tf` files in a directory form a single module. There is no single manifest file — declarations are spread across files by convention (e.g., `main.tf`, `variables.tf`, `outputs.tf`). The root module is the working directory; child modules are called explicitly.

**Variables and outputs** create explicit interfaces at module boundaries. Inputs are declared with type constraints; outputs are the only values a calling module can read. This enforces encapsulation — modules are black boxes with typed ports.

**Workspaces** allow a single configuration to manage multiple environment instances (e.g., dev/staging/prod) with isolated state files. This is lighter-weight than maintaining separate directory trees per environment.

### 5. State and Reproducibility

The **`terraform.tfstate`** file is Terraform's record of what it has provisioned. The official purpose: "to store bindings between objects in a remote system and resource instances declared in your configuration." Without state, Terraform cannot compute diffs — it has no way to know that `aws_instance.web` in config corresponds to `i-0abc123` in AWS. ([developer.hashicorp.com/terraform/language/state](https://developer.hashicorp.com/terraform/language/state))

State enables **drift detection**. `terraform plan` refreshes the state by querying real infrastructure, then computes the diff between refreshed state and declared config. Out-of-band changes (manual console edits, other automation tools) show up as unexpected plan output. `terraform plan -refresh-only` updates state without proposing infrastructure changes — useful for reconciling drift without acting on it.

Local state is the default but unsuitable for teams: it cannot be locked (concurrent applies corrupt state), and loss of the file means loss of all resource bindings. The recommended path is a **remote backend** (HCP Terraform, S3 + DynamoDB, etc.) with state locking. ([developer.hashicorp.com/terraform/language/state](https://developer.hashicorp.com/terraform/language/state))

### 6. Extensibility

The **provider model** is Terraform's primary extension mechanism. Providers are plugins — separately compiled binaries that implement the create/read/update/delete operations for a specific API. HashiCorp maintains official providers (AWS, Azure, GCP, Kubernetes); HashiCorp partners maintain verified providers; the community maintains open providers. Anyone with a GitHub account can publish to the Terraform Registry. ([developer.hashicorp.com/terraform/registry/providers/publishing](https://developer.hashicorp.com/terraform/registry/providers/publishing))

Provider design principles (official):
- A provider should manage a single API or problem domain.
- A resource should map to a single API object (not a complex abstraction).
- Schema should follow the underlying API naming — not Terraform-specific terminology.
- Resources must be importable (`terraform import`) to support brownfield workflows.
([developer.hashicorp.com/terraform/plugin/best-practices/hashicorp-provider-design-principles](https://developer.hashicorp.com/terraform/plugin/best-practices/hashicorp-provider-design-principles))

`terraform init` downloads providers declared in `required_providers` blocks, verifying signatures during download. Provider source addresses use a three-part scheme: `hostname/namespace/type` (defaulting to `registry.terraform.io`). ([hashicorp.com/blog/automatic-installation-of-third-party-providers-with-terraform-0-13](https://www.hashicorp.com/en/blog/automatic-installation-of-third-party-providers-with-terraform-0-13))

**Modules** are the reuse mechanism above the resource level. They group related resources into named, parameterized units. Modules can be sourced from local paths, Git repositories, or the Terraform Registry. Public and private registries coexist — HCP Terraform and Terraform Enterprise include a private module registry for internal sharing. ([developer.hashicorp.com/terraform/language/modules](https://developer.hashicorp.com/terraform/language/modules))

### 7. Notable Tradeoffs and Criticisms

**State file as a footgun.** The state file is a plaintext JSON file that can contain secrets (database passwords, private keys) written by providers that do not mark values as sensitive. State stored locally is easily lost; state stored remotely requires managing backend config and access credentials. Corrupted or deleted state is one of the most painful operational incidents in the Terraform ecosystem — resources become "orphaned" (real infrastructure exists but Terraform has no record of it).

**Drift accumulates silently.** Auto-scaling, managed services, and external automation all create drift by design. Terraform does not watch for drift continuously — it only detects it at plan time. Left unchecked, drift causes surprise plan outputs, compliance violations, and cases where Terraform wants to destroy a database serving live traffic.

**Slow feedback loop.** `terraform plan` against a large infrastructure footprint can take minutes because it must query every managed resource's current state. The plan/apply gate is valuable but costly at scale.

**State locking is backend-dependent.** Local state has no locking. Remote backends implement it differently. Concurrent applies without locking corrupt state; with locking, one apply blocks all others. There is no fine-grained locking below the workspace level.

HashiCorp responded to many of these over time: `terraform plan -refresh-only`, `moved` blocks for refactoring without destroy/recreate, `sensitive = true` on variable and output blocks, and eventually full support for remote state with locking in the open-source CLI.

---

## Shared Principles Worth Borrowing

The following patterns appear independently in both tools and represent genuine cross-cutting design wisdom:

- **Manifest at repo root.** A single, human-readable file (`package.json` / root `.tf` files) declares all intent. New contributors start there. Automation starts there. The manifest is the contract.

- **Lockfile for reproducibility.** The manifest declares ranges or constraints; the lockfile pins exact resolved state. These are different concerns and should be different files. Commit both. The lockfile is load-bearing, not optional.

- **Init before everything.** A dedicated `init` command resolves and downloads dependencies before any operational commands run. This front-loads failure to a safe, read-only phase. It also generates or updates the lockfile.

- **Plan before apply.** Separating "compute what will change" from "make the change" is one of the most user-trust-building patterns in CLI design. Operators can review, script around, or abort. Apply with auto-approval is an opt-in escape hatch, not the default.

- **Idempotent verbs.** Both `npm install` and `terraform apply` converge to declared state regardless of how many times they are run. Repeated invocations are safe. This property makes them composable in automation.

- **Declarative configuration, not scripts.** Users declare desired end state; the tool determines the sequence of operations. This inverts the traditional "write a script that does steps 1-N" model and makes configurations human-auditable.

- **Registry + provider/plugin extensibility.** Neither tool tries to bundle all capabilities. Both delegate extension to third parties via a public registry with a defined publishing protocol. The CLI discovers and downloads extensions at init time; extension authors do not need to patch the core tool.

- **Scoped namespacing.** npm scopes (`@org/package`) and Terraform provider source addresses (`registry.terraform.io/hashicorp/aws`) prevent naming collisions and enable multiple registries (public + private) to coexist transparently.

- **Version constraints with lockfile resolution.** Declaring a version range (SemVer caret, Terraform `~>`) expresses intent; the lockfile records the resolved fact. This decouples "what versions are acceptable" from "what version is installed right now."

- **Human-readable + machine-readable output modes.** Both tools output for humans by default (colored, annotated, conversational). Both provide machine-parseable alternatives (`--json`, `-out=plan.bin`) for CI and scripting. Designing both modes from the start is cheaper than retrofitting.

- **Codification as documentation.** The configuration file is the documentation. If it is in the repo, it is versioned, reviewable, and auditable. Undocumented tribal knowledge living outside the repo is a failure mode both tools are designed to eliminate.

- **Brownfield support.** npm's lockfile can represent any installed state; Terraform's `terraform import` brings existing resources under management. Both acknowledge that the world does not start from zero.

---

## Sources

- [Tao of HashiCorp](https://www.hashicorp.com/en/tao-of-hashicorp) — HashiCorp's eight founding principles
- [Terraform Introduction](https://developer.hashicorp.com/terraform/intro) — official overview, declarative model, provider architecture
- [Terraform Core Workflow](https://developer.hashicorp.com/terraform/intro/core-workflow) — write/plan/apply rationale
- [Terraform State](https://developer.hashicorp.com/terraform/language/state) — state purpose and tradeoffs
- [Terraform Provider Requirements](https://developer.hashicorp.com/terraform/language/providers/requirements) — version locking, `.terraform.lock.hcl`
- [Terraform Provider Design Principles](https://developer.hashicorp.com/terraform/plugin/best-practices/hashicorp-provider-design-principles) — official nine provider design rules
- [Terraform Modules](https://developer.hashicorp.com/terraform/language/modules) — reuse and registry sourcing
- [Automatic Installation of Third-Party Providers (Terraform 0.13)](https://www.hashicorp.com/en/blog/automatic-installation-of-third-party-providers-with-terraform-0-13) — registry extension model
- [HCL GitHub README](https://github.com/hashicorp/hcl) — design rationale for HCL vs JSON/YAML
- [Unix Philosophy and Node.js — Isaac Schlueter](https://blog.izs.me/2013/04/unix-philosophy-and-nodejs/) — primary source for npm's small-modules philosophy
- [Interview with Isaac Z. Schlueter — Increment](https://increment.com/development/interview-with-isaac-z-schlueter-ceo-of-npm/) — npm origin story and flexibility philosophy
- [About npm](https://docs.npmjs.com/about-npm) — official npm purpose statement
- [package-lock.json docs](https://docs.npmjs.com/cli/v11/configuring-npm/package-lock-json/) — lockfile design rationale and guarantees
- [npm Workspaces](https://docs.npmjs.com/cli/v11/using-npm/workspaces/) — monorepo workspace design
- [npm Scope](https://docs.npmjs.com/cli/v11/using-npm/scope/) — scoped packages and registry routing
- [SemVer tilde and caret — NodeSource](https://nodesource.com/blog/semver-tilde-and-caret) — version range design and philosophy
