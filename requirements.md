# **Project: Universal Email Analytics (UEA) \- Requirements Document**

## **1\. Introduction**

The **Universal Email Analytics (UEA)** application is a high-performance, self-hosted web application built with a robust **Golang** backend and a modern **ReactJS** frontend. Designed as a standalone, comprehensive dashboard, UEA facilitates the aggregation and exploration of email data across disparate providers via the **IMAP protocol**.

By centralizing data into a local-first environment, UEA empowers users to perform deep-dive analytics—such as trend discovery, social network mapping, and semantic content search—without sacrificing privacy. The application is tailored for power users, researchers, and professionals who manage high-velocity inboxes and require more than what standard webmail clients offer in terms of data visibility and automated insight.

## **2\. Backend Requirements (Golang)**

### **2.1. Multi-Account Sync Engine**

* **1. Intelligent Worker Pool with Real-Time Telemetry:** Implement a sophisticated worker pool architecture that manages concurrency on a per-host basis, coupled with deep observability.
  * **Per-Host Rate Limiting:** The engine must cap concurrent connections to a single provider (e.g., maximum 10 to `imap.gmail.com`) to respect rate limits and prevent temporary IP blacklisting, even if the global dispatcher can handle 50+ concurrent Goroutines.
  * **State Introspection API:** Expose an internal HTTP endpoint (e.g., using Go's `expvar` or Prometheus metrics) that broadcasts the real-time state of the worker pool. This must show active vs. idle workers, current host allocations, and individual worker states (`CONNECTING`, `AUTHENTICATING`, `IDLE`, `FETCHING`, `BACKOFF`).
  * **Deadletter & Panic Recovery:** Implement robust panic recovery per worker. If a worker crashes, the dispatcher must log the stack trace, record the specific IMAP command that caused the failure, and gracefully spin up a replacement worker without halting the entire pool.
* **2. Stateful Incremental Sync and Attempt Auditing:** Utilize IMAP UIDs, `MODSEQ`, and `UIDVALIDITY` to track synchronization state, backed by a persistent audit trail of all sync activities.
  * **High-Water Mark Tracking:** The engine should only fetch new headers or flags since the last recorded state, significantly reducing bandwidth. It must explicitly handle `UIDVALIDITY` changes to automatically trigger full re-syncs when mailboxes are repacked.
  * **Sync Attempt Ledger:** Every synchronization attempt must be logged in a dedicated SQLite table (e.g., `sync_history`). This ledger must record:
    * Target account and folder.
    * Start and end timestamps.
    * Bytes transferred and messages processed.
    * The outcome: `SUCCESS`, `PARTIAL_SUCCESS` (e.g., network timeout halfway through), or `FAILURE` with the specific error code/message.
  * **Dry-Run Mode:** Implement a CLI flag or configuration option for a "dry run." In this mode, the engine negotiates the sync state and logs the UIDs it *would* fetch or update, without actually downloading the payloads or mutating the local database.
* **3. Deduplication Logic with Deterministic Debugging:** Implement a content-aware hashing algorithm (e.g., SHA-256) on normalized message bodies and unique `Message-ID` headers to ensure cross-folder and cross-account deduplication.
  * **MIME Normalization Pipeline:** The engine must reliably strip whitespace, normalize encodings, and remove variable MIME boundaries before hashing.
  * **Hash Explainability:** In debug mode, the engine must be able to dump the exact normalized string that was fed into the SHA-256 function. This is critical for diagnosing "false negatives" where two seemingly identical emails yield different hashes due to invisible formatting or client-specific headers.
  * **Collision Logging:** While SHA-256 collisions are statistically negligible, the database layer should enforce unique constraints and log an explicit `WARN` if two different raw emails ever generate the same hash, allowing for immediate manual inspection.
* **4. Credential Vault and Safe Testing Harness:** All IMAP and SMTP credentials must be encrypted at rest using AES-256-GCM, with the key derived via Argon2id.
  * **Secure Logging:** The logging system must be context-aware and strictly redact all plaintext passwords, session tokens, or OAuth bearer tokens from console output and log files, even at the `TRACE` log level.
  * **Auth Failure Auditing:** Differentiate between network timeouts and legitimate `NO/BAD` authentication rejections from the IMAP server. Auth failures must be surfaced immediately to the sync ledger to prevent the worker pool from endlessly hammering a server with bad credentials and triggering an account lockout.
  * **Mock Vault for CI/CD:** Provide an in-memory, unencrypted implementation of the `CredentialVault` interface specifically for unit and integration testing, completely bypassing the Argon2id/AES overhead during automated test suites.

### **2.2. Data Persistence & Hybrid Search**

* **Optimized SQLite Core:** The database must use Write-Ahead Logging (WAL) and synchronous "NORMAL" mode to balance performance and data integrity. Tables for messages, participants, and threads should be highly normalized to facilitate rapid joins during cross-filtering.  
* **The Hybrid Search Architecture:**  
  * **Lexical Layer (FTS5):** Leverage SQLite's FTS5 extension to provide lightning-fast, exact keyword matching. This layer handles specific queries like from:jdoe@example.com, has:attachment, and standard boolean searches ("project alpha" AND NOT "draft").  
  * **Semantic Layer (Vector Index):** Integrate a Go-native vector library or the sqlite-vss extension. Every message body is transformed into a high-dimensional vector (384 or 768 dimensions) using a local embedding model (e.g., all-MiniLM-L6-v2).  
  * **Rank Fusion:** Implement **Reciprocal Rank Fusion (RRF)** to synthesize results. If a user searches for "Travel plans," FTS5 finds messages containing the word "Travel," while the vector index finds messages about "flights," "hotels," and "itineraries," merging them into a single, relevant results list.  
* **Chunking Strategy:** For long emails (e.g., newsletters or long threads), the system must split text into overlapping chunks (e.g., 512 tokens with a 50-token overlap) to ensure that semantic meaning is captured even for content buried deep in a message.

### **2.3. API & AI Gateway**

* **Materialized Analytics Views:** To ensure the UI remains responsive, complex analytical queries (like topic prevalence or sender volume) should not be calculated on-the-fly. The backend must maintain materialized summary tables that are updated asynchronously during sync cycles.  
* **LLM Abstraction Layer:** Provide a unified interface for multiple AI backends. Users can choose between local execution (via **Ollama** or **llama.cpp** sidecars) for maximum privacy, or high-performance remote APIs (OpenAI, Anthropic, Gemini).  
* **Streaming Responses:** The API should support Server-Sent Events (SSE) for the "Bullet-to-Draft" feature, allowing the UI to render the AI-generated response in real-time.

### **2.4. CLI Management Suite**

The uea binary serves as both the web server and a powerful administrative tool:

* **uea account**: Commands to add (--host \--user \--pass), list, remove, or verify connections.  
* **uea doctor**: A comprehensive diagnostic suite that checks local disk health, database indices, LLM connectivity, and IMAP reachability.  
* **uea maintenance**: Commands for reindex-vectors (to upgrade embedding models) and vacuum (to reclaim disk space).  
* **uea backup**:  
  * **Atomic Snapshot:** Uses the sqlite3\_backup API to create a consistent file copy while the application is running.  
  * **Granular Extraction:** A utility to export specific threads or subsets of messages to standardized formats like .eml or .json.

## **3\. Design & User Interface**

### **3.1. General Interface Architecture**

The application adopts a **Master-Detail-Filter** layout.

*   **Global Sidebar**: Provides high-level navigation between the Dashboard, Mailbox, Search, and Settings.  
*   **Dynamic Filter Bar**: A persistent top-level bar that aggregates all active filters into distinct, removable pillboxes (e.g., `[Date: 2026-02-25 x]`, `[From: alice@tech.com x]`). It supports deep cross-filtering, allowing users to drill down by selecting points of interest in the dashboard, which instantly filters both the analytics and the unified Mailbox feed.

### **3.2. Primary Views & Layouts**

#### **3.2.1. The Analytics Pulse (Main Dashboard)**

The "Pulse" is the primary engine for data discovery. It is composed of interactive widgets that communicate via a shared Zustand state.

*   **Temporal Volume Heatmap**: A calendar-based heat map (via `@nivo/calendar`) showing message density over the year. Clicking a specific day instantly filters all other widgets and the Mailbox to that date.
*   **Top Senders List**: A ranked breakdown of where mail is coming from, automatically excluding the user's own addresses. Clicking a sender applies a cross-filter.
*   **The "Responsiveness" Gauge (Productivity Focus)**: Helps users understand their own communication efficiency.
    *   *Average Response Time (ART)*: A radial gauge or simple metric card (using `@nivo/bullet`) tracking response time against personal benchmarks or SLAs.
    *   *Response Funnel*: A horizontal funnel chart tracking the flow: Received → Opened → Replied → Resolved. Clicking "Resolved" filters the list to show threads where the last message was from the user.
*   **Flow & Relationship Mapping (Network View)**: Identifies hidden bottlenecks or key collaborators.
    *   *Sankey Diagram*: Utilizes `@nivo/sankey` to visualize the flow of messages between departments or key contact groups, showing where attention capital is spent.
    *   *Thread Depth Histogram*: A bar chart showing the distribution of thread lengths to identify communication bottlenecks.
*   **Peak Activity & "Quiet Hours" (Energy Management)**: Focuses on daily and weekly rhythms.
    *   *Hourly Density Punchcard*: A 24-hour grid (Monday–Sunday) showing when the user is most active.
    *   *The "Backlog" Counter*: A real-time single-metric widget showing the current number of unread or unanswered emails relative to a daily goal.

**Advanced AI Insights & Automation Widgets:**

*   **Advanced Topic Discovery & Clustering**: Moves beyond basic keyword counting to organize content contextually.
    *   *Pillar-Cluster Hierarchies*: Organizes topics around a central "content pillar" (e.g., "Family Logistics") with surrounding subtopic "clusters" (e.g., "Christmas List," "Church Activities").
    *   *Latent Dirichlet Allocation (LDA) Visualization*: Uses LDA to automatically discover latent topics, assigning topic probabilities to each thread to infer meaning from word patterns.
    *   *Semantic Relationship Mapping*: Uses link visualization or chord diagrams to show how different topics or entities are interconnected, highlighting communication bottlenecks or frequent collaborators.
*   **Deep Sentiment & Emotional Intensity**: Categorizes the emotional tone of emails.
    *   *Sentiment Trend & Score Widget*: A line chart showing how sentiment fluctuates over time to detect early signs of frustration before they escalate.
    *   *Relationship Portraits*: A multi-layered visual (like "Themail") where topical words vary in color and size to portray the tone of a specific relationship over different timeframes (years vs. months).
    *   *Emotional Categorization*: Identifies distinct emotions like joy, anger, or sadness by analyzing syntactic cues and context, rather than just binary positive/negative scoring.
*   **Entity Extraction & Automation Widgets**: Turns emails into structured data.
    *   *Automated Information Extraction*: Widgets that pull structured data from unstructured text, such as vendor names, amounts, and due dates from receipts or withdrawal notifications.
    *   *Semantic Search Filter*: Allows querying the dashboard for complex concepts (e.g., "emails where my boss appears to be frustrated") by understanding intent and context rather than exact word matches.

**Interactive Widget Logic (Zustand Integration):**

*   *Cross-Widget Filtering*: Provides highly synchronized data exploration. For example, clicking a sentiment category (e.g., "Frustrated") instantly updates the Topic Trends to show which subjects are driving that negative sentiment.
*   *Modal Insights*: Uses Zustand to manage modals that appear when clicking an entity, displaying a summary of all recent interactions and sentiment scores associated with that specific contact.

**Architectural Approach for the Analytics Pulse:**

To support heavy text analysis alongside real-time frontend filtering, the system requires a decoupled architecture:

*   **1. High-Level System Architecture:**
    *   **The Presentation Layer (React + Zustand + Nivo):** Handles rendering, state management, and user interactions. Queries the backend for aggregated, visualization-ready data.
    *   **The API & Orchestration Layer (Golang):** Serves as the high-performance traffic cop handling authentication, database queries, and orchestrating data ingestion from the mail server.
    *   **The AI & Data Science Engine:** Isolates deep learning and NLP workloads (like LDA) into a separate microservice or serverless pipeline.
*   **2. Frontend Strategy:**
    *   **State Management Design (Zustand):** Create a single, centralized slice-based store (`usePulseStore`).
        *   *Global Filters State*: Store current `selectedDate`, `selectedSender`, `selectedTopic`, and `selectedSentiment`.
        *   *Action Dispatchers*: Clicking a widget calls actions like `setDateFilter` in Zustand.
        *   *Reactive Selectors*: Widgets subscribe only to specific filter states, automatically appending filters to their API fetches or native data filtering.
    *   **Visualization Implementation:**
        *   *Nivo Wrappers*: Build standard wrapper components around `@nivo` charts that automatically hook into the Zustand store for global theme and filter context.
        *   *Modal Management*: Store `modalContent` and `isModalOpen` state in Zustand.
*   **3. Backend Strategy:**
    *   **Concurrency for Ingestion:** Use Go's goroutines/channels to build a concurrent email ingestion pipeline (one pool for DB insertion, another for AI engine pushing).
    *   **Calculating Responsiveness:** Track Received, Opened, Replied, and Resolved timestamps in SQLite/PostgreSQL. Use SQL window functions to rapidly aggregate the Average Response Time (ART).
    *   **Peak Activity Pre-aggregation:** Pre-aggregate hourly message densities in the database so the Go API can serve the pre-computed grid instantly for the Punchcard.
*   **4. AI & Advanced Insights Engine:**
    *   **Topic Discovery & LDA:** Utilize a dedicated Python service for daily batch jobs using NLP libraries to perform LDA over the corpus, saving topics back to the primary database.
    *   **Deep Sentiment & Emotion:** Deploy open-source LLMs locally (via Ollama) or use managed infrastructure. Pass threads through models fine-tuned for multi-label emotion classification (Joy, Anger, Sadness).
    *   **Semantic Search & Entity Extraction:** Generate vector embeddings for emails and store them in a vector database. Use orchestration frameworks (like LangChain) to translate intent into vector queries. Prompt lightweight LLMs to output strict JSON schemas for extracted entities (receipts, due dates) for Go to validate.
*   **5. Suggested Phased Implementation:**
    *   **Phase 1: The Foundation.** Build the Go REST API, the React/Zustand shell, and the core database. Implement basic metrics and simple Nivo charts.
    *   **Phase 2: The Interactive Matrix.** Implement the Zustand cross-filtering logic. Connect the Heatmap, Sankey, and Funnel so interacting with one dynamically reshapes the others.
    *   **Phase 3: The AI Integration.** Stand up the NLP pipeline, route data through the vector database for semantic search, and implement LLM inferences for sentiment scoring and LDA clustering.

#### **3.2.2. The Mailbox (The Feed)**

* **High-Performance Feed:** Implements React Virtualization to handle scrolling through hundreds of thousands of entries with zero lag.  
* **Contextual Actions:** Quick-action buttons on each list item allow for "Single-Click Search for Related" or "Extract Attachments."  
* **Smart Snippets:** Instead of just the first line of an email, the list shows an AI-generated 1-sentence summary that highlights the core "ask" or "info" in the message.

#### **3.2.3. Thread Focus View**

* **Conversational UI:** Threads are reconstructed chronologically and rendered as a chat interface, stripping away redundant headers and signatures to focus on the dialogue.  
* **Intelligence Side-Panel:** While a user reads, this panel displays "Social Insights" (e.g., "This sender is in your Top 5% of contacts") and "Contextual Links" (e.g., links to previous threads regarding the same topic).  
* **AI Quick-Compose:** A dedicated text area for the **Bullet-to-Draft** workflow.

#### **3.2.4. Visual AI Agent Builder**

* **Eino Integration:** Provides a graphical interface for defining and extending AI agents powered by the Eino framework.
* **React Flow Canvas:** A drag-and-drop node-based editor for wiring nodes into complex agent topologies.
* **Node Palette:** A dedicated panel on the left side of the interface allows users to drag and drop new nodes into the agent definition graph.
* **Node Settings Panel:** A dedicated panel on the right side of the interface that displays and allows modification of configuration settings for the currently selected node in the canvas.
* **Node Types & Settings:**
  * **Core Operational Nodes:**
    * **ChatModel:** The primary "reasoning" node that interacts with LLMs to process messages and generate responses.
      * *Settings:* `Model Provider` (e.g., Local, OpenAI, Anthropic), `Model Name` (e.g., llama3, gpt-4o), `Temperature` (0.0 to 1.0 slider), `Max Tokens` (integer limit), `System Prompt Override`.
    * **ToolsNode:** A specialized node used to invoke external functions, APIs, or tools. It handles the execution of capabilities identified by the ChatModel.
      * *Settings:* `Available Tools` (multi-select checklist: e.g., web_search, read_email, send_email), `Timeout` (ms), `Retry Count`.
    * **ChatTemplate:** Used to format user inputs and system instructions into structured message prompts before they reach the ChatModel.
      * *Settings:* `System Message Template` (textarea), `User Message Template` (textarea), `Input Variables` (dynamic key-value mapping list).
  * **Data & Context Nodes:**
    * **Retriever:** Essential for RAG (Retrieval-Augmented Generation) workflows; this node fetches relevant documents or data from a vector database or knowledge base.
      * *Settings:* `Data Source` (e.g., Email DB, Local Docs), `Top K Results` (integer), `Similarity Threshold` (0.0 to 1.0), `Query Template`.
    * **Embedding:** Converts text into vector representations, often used in conjunction with a Retriever for semantic search.
      * *Settings:* `Embedding Model` (e.g., all-MiniLM-L6-v2), `Chunk Size` (tokens), `Chunk Overlap` (tokens).
    * **Lambda:** A flexible node for executing custom Go code logic, such as data transformation or specialized business rules, within the graph flow.
      * *Settings:* `Function Code` (monaco editor/textarea for Go code), `Input Mapping`, `Output Mapping`.
  * **Specialized Agent Patterns:**
    * **ReAct Agent Node:** A high-level abstraction that combines reasoning and acting in a loop. It typically encapsulates a ChatModel and ToolsNode with a predefined topology.
      * *Settings:* `Max Iterations`, `Model Selection` (inherits ChatModel settings), `Tools Selection` (inherits ToolsNode settings).
    * **Workflow Agent Nodes:** Used for multi-agent orchestration, these include:
      * **SequentialAgent:** Executes sub-agents in a fixed order.
        * *Settings:* `Agent Sequence` (ordered list of selected sub-agents).
      * **ParallelAgent:** Runs multiple sub-agents concurrently.
        * *Settings:* `Target Agents` (multi-select), `Aggregation Strategy` (e.g., Concat, Reduce).
      * **LoopAgent:** Iterates through a set of sub-agents until a condition is met.
        * *Settings:* `Condition Evaluation` (Lambda/code logic), `Max Loops`.
  * **Graph Plumbing:**
    * **START / END:** Virtual nodes that define the entry and exit points of your graph.
      * *Settings:* `Expected Input Schema` (for START), `Output Schema` (for END).
    * **Branch:** While often implemented as a conditional edge, a branch logic point determines the next node based on runtime state (e.g., deciding whether to call a tool or finish the task).
      * *Settings:* `Condition Map` (mapping of output states to target node IDs).
* **Node Connections & Data Flow (Edges):** In the Eino framework, connecting nodes involves defining Edges that serve as channels for data flow and control logic. User behavior must support the following:
  * **Ensure Type Alignment:**
    * *Exact Match:* Upstream and downstream types should ideally be identical (e.g., both use `*schema.Message`).
    * *Interface Implementation:* A connection is valid if the upstream concrete type implements the interface required by the downstream node.
    * *Type Conversion:* If types do not align, use Eino's `WithOutputKey` option to convert an output into a `map[string]any` that the downstream node can process.
  * **Define the Execution Path:** Explicitly define how information moves through the graph to control behavior:
    * *Establish Entry and Exit:* Every graph must connect from a designated `START` node to receive initial user input, and route to an `END` node to return the final response.
    * *Direct Edges:* Use direct connections for linear logic, such as passing a prompt from a `ChatTemplate` to a `ChatModel`.
    * *Conditional Branching:* For non-linear agents (like a ReAct loop), define Branches after a node. This determines at runtime whether to follow one path (e.g., go to `ToolsNode`) or another (e.g., go to `END`) based on the model's output.
  * **Manage State and Data Flow:** Support sophisticated data handling during node connection:
    * *Field-Level Mapping:* In a Workflow, allow users to map specific output fields from multiple predecessor nodes into a single input for the next node.
    * *Global State:* Instead of passing every piece of data through edges, enable storing shared information (like chat history) in a global State that nodes can read from or write to independently.
    * *Passthrough Nodes:* Use Passthrough nodes to maintain data flow in parallel branches where one branch has fewer functional nodes than another, ensuring both branches synchronize correctly.
  * **Visual vs. Code Orchestration:** Support generating standard graph definitions from the UI.
    * *Visual Orchestration:* Use the React Flow canvas to visually drag connections between node ports.
    * *Code-Based Generation:* The visual topology must successfully serialize into a format that allows the backend to instantiate the graph via Eino's `GraphBuilder` (e.g., using `AddEdge` or `AddBranch`).
* **Persistence:** Serializes canvas configurations into strict JSON definitions stored in the SQLite database.

## **4\. Workflows**

### **4.1. Cross-Filtering & Discovery**

1. User clicks a "Spike" in the Volume Chart (January 2024).  
2. The Sender Chart updates to show who was active in January.  
3. User clicks "Topic: Real Estate" in the Treemap.  
4. The Message List now only shows real-estate-related emails from January 2024\.  
5. This "Drill-down" approach allows users to find specific needles in the haystack without typing a single search query.

### **4.2. AI-Assisted Response (Bullet-to-Draft)**

* **The Workflow:** A user reads a complex inquiry and types three bullet points into the quick reply box: "Can't make Monday," "Available Tuesday 2 PM," "Send the PDF first."  
* **The Generation:** The user clicks **Synthesize**. The system sends the last 5 messages of the thread \+ the 3 bullets \+ a "Persona Profile" (e.g., Professional) to the LLM.  
* **The Result:** The AI generates a 3-paragraph email that gracefully declines the Monday meeting, proposes the Tuesday slot, and requests the document. The user reviews and clicks **Send**.

### **4.3. Secure Cloud Backup & Restore**

* **Encryption:** The system generates a unique 256-bit encryption key from the master passphrase.  
* **The Backup:** Data is compressed, encrypted, and streamed to an S3-compatible bucket (AWS, MinIO, or Cloudflare R2).  
* **The Restore:** Users can view a "Timeline of Snapshots." They can choose to restore the entire DB to a new machine or "Deep-Dive" into a snapshot to find and restore a single deleted message or attachment.

## **5\. Non-Functional Requirements**

### **5.1. Performance & Scalability**

* **Latency Targets:** Lexical searches must return in \<100ms. Semantic searches, involving vector distance calculations, must return in \<400ms for datasets of up to 500k messages.  
* **Resource Efficiency:** The Go backend must utilize a "Buffer Pool" for SQLite operations to minimize disk I/O, keeping total system RAM usage under 1GB during idle background syncing.

### **5.2. Installation & Privacy**

* **Zero-Dependency Binary:** The application is distributed as a single executable for Windows, macOS (Intel/Silicon), and Linux. All HTML, CSS, and JS assets are embedded using go:embed.  
* **Privacy First:** All vector embeddings and topic models are generated locally. No email content is ever sent to a third party unless the user explicitly configures a remote LLM API (like OpenAI).

## **6\. Development & Quality**

* **CI/CD Pipeline:** Automated GitHub Actions trigger builds for all target architectures on every tagged release.  
* **Integration Testing:** Uses a "Mock IMAP Server" to verify that the sync engine handles edge cases like connection drops, malformed headers, and large attachment handling.  
* **Observability:** Implement internal metrics (Prometheus format) for tracking sync speed, search latency, and database growth.