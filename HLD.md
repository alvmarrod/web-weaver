Below is a **concise High-Level Design (HLD)** suitable as a system definition page.

---

# Web Crawler – High Level Design (HLD)

## 1. Purpose

Build a lightweight web crawler that discovers domains and subdomains and their outbound cross-domain links, producing a dataset suitable for visualization as an **Internet graph**.

---

## 2. Core Concepts

### 2.1 Node Definition

* **Node = domain or subdomain**

  * Examples: `example.com`, `blog.example.com`
* Domains and subdomains are treated as **distinct nodes**
* URL paths, query strings, and fragments are discarded

### 2.2 Page Role

* Pages are **not nodes**
* Pages are used only to:

  * extract a node description
  * discover outbound links

---

## 3. Data Model

### 3.1 Node

* `node_id` (int)
* `domain_name` (string)
* `description` (string, max 60 chars)

  * Source: `<title>` preferred, fallback to `<meta description>`
* `crawl_count` (int, max = 3)

### 3.2 Edge

* `from_node_id`
* `to_node_id`
* `weight`

  * Number of distinct pages observed linking from source node to target node

---

## 4. Crawling Rules

### 4.1 Crawl Limits

* **Max crawls per node**: `N = 3`
* **Max subdomains per root domain**: `3`
* **Depth limit**: `5`
* **Max outbound links per page**: `10` (cross-domain only)

### 4.2 Loop Prevention

* Maintain `(node_id, crawl_count)`
* A node is crawled only while `crawl_count < 3`
* Edges may be recorded multiple times; crawling is limited

This prevents infinite loops such as:

```text
A → B → A → B
```

---

## 5. Link Extraction & Selection

### 5.1 Link Filtering

* Only **outbound links** (target domain ≠ source domain)
* Ignore:

  * social media
  * ads
  * analytics
  * known boilerplate domains

### 5.2 Link Selection Heuristic (Simple)

From a page:

1. Deduplicate by target domain
2. Keep the first ≤ 10 unique outbound domains
3. Order based on first appearance in the DOM

---

## 6. Crawl Strategy

* **Breadth-first search (BFS)**
* Queue entries contain:

  * `node_id`
  * `current_depth`
* Stop crawling when:

  * depth > 5
  * crawl budget exhausted
  * subdomain limit exceeded

---

## 7. Graph Semantics

* A directed edge `A → B` exists if **any page** under node A links to node B
* Edge weight represents **link strength**
* Graph is suitable for:

  * force-directed layouts
  * domain influence analysis
  * macro web structure visualization

---

## 8. Non-Goals

* Full page indexing
* Intra-domain link analysis
* SEO completeness or ranking accuracy

---

This HLD fully defines scope, limits, semantics, and behavior and is sufficient to implement the crawler and graph generator consistently.

If you want next:

* a **crawler loop pseudocode**
* a **graph schema for Neo4j / SQL**
* or a **sample crawl walk-through**
