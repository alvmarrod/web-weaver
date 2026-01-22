"""
SQLite Database Loader for NetworkX Graphs

This module handles loading graph data from SQLite databases created by
the web-weaver crawler into NetworkX graph objects. It provides functions
for data loading, filtering, and statistical analysis.

Features:
- Backward compatibility with different database schema versions
- Safe text encoding handling
- Graph filtering and manipulation
- Statistical analysis functions
"""

import sqlite3
from typing import Dict, List, Tuple, Union

import networkx as nx


def _safe_decode_text(text: Union[str, bytes, None]) -> str:
    """
    Safely decode text that might have encoding issues.

    Handles various text input types and encoding problems that can
    occur when reading from SQLite databases with mixed encodings.

    Args:
        text: Text to decode (string, bytes, or None)

    Returns:
        Safely decoded string (empty string if input is None)
    """
    if text is None:
        return ""

    if isinstance(text, str):
        return text

    # Handle bytes with encoding fallback
    if isinstance(text, bytes):
        try:
            return text.decode('utf-8')
        except UnicodeDecodeError:
            # Fallback: replace problematic characters
            return text.decode('utf-8', errors='replace')

    return str(text)


def _detect_database_schema(cursor: sqlite3.Cursor) -> Dict[str, bool]:
    """
    Detect available columns in the database schema.

    Args:
        cursor: SQLite database cursor

    Returns:
        Dictionary indicating which optional columns are available
    """
    # Check nodes table structure
    cursor.execute("PRAGMA table_info(nodes)")
    node_columns = {col[1] for col in cursor.fetchall()}

    # Check edges table structure
    cursor.execute("PRAGMA table_info(edges)")
    edge_columns = {col[1] for col in cursor.fetchall()}

    return {
        'has_depth': 'last_depth' in node_columns,
        'has_created_at': 'created_at' in node_columns,
        'has_edge_id': 'edge_id' in edge_columns
    }


def _load_nodes_from_database(cursor: sqlite3.Cursor, schema_info: Dict[str, bool]) -> List[Tuple]:
    """
    Load node data from database with schema-aware queries.

    Args:
        cursor: SQLite database cursor
        schema_info: Information about available database columns

    Returns:
        List of node tuples from database
    """
    if schema_info['has_depth']:
        # Modern schema with depth information
        cursor.execute("""
            SELECT node_id, domain_name, description, crawl_count, last_depth
            FROM nodes
            ORDER BY node_id
        """)
        return cursor.fetchall()
    else:
        # Legacy schema without depth
        cursor.execute("""
            SELECT node_id, domain_name, description, crawl_count
            FROM nodes
            ORDER BY node_id
        """)
        return cursor.fetchall()


def _load_edges_from_database(cursor: sqlite3.Cursor) -> List[Tuple]:
    """
    Load edge data from database.

    Args:
        cursor: SQLite database cursor

    Returns:
        List of edge tuples from database
    """
    cursor.execute("""
        SELECT from_node_id, to_node_id, weight
        FROM edges
        ORDER BY from_node_id, to_node_id
    """)
    return cursor.fetchall()

def load_graph(db_path: str) -> nx.DiGraph:
    """
    Load nodes and edges from SQLite database into NetworkX directed graph.

    Supports multiple database schema versions and handles encoding issues
    gracefully. The function is backward compatible with databases created
    by different versions of the web-weaver crawler.

    Args:
        db_path: Path to the SQLite crawler database file

    Returns:
        NetworkX DiGraph with nodes and edges loaded from database

    Raises:
        sqlite3.Error: If database cannot be opened or queried
        ValueError: If database has unexpected structure
    """
    print(f"📋 Loading database: {db_path}")

    # Open database connection with safe text handling
    conn = sqlite3.connect(db_path)
    conn.text_factory = lambda x: _safe_decode_text(x)
    cursor = conn.cursor()

    try:
        # Create directed graph
        G = nx.DiGraph()

        # Detect database schema capabilities
        schema_info = _detect_database_schema(cursor)

        if not schema_info['has_depth']:
            print("  ⚠️  Note: Using legacy database format (pre-v0.3.0, no depth info)")

        # Load nodes with schema-appropriate query
        node_rows = _load_nodes_from_database(cursor, schema_info)

        for row in node_rows:
            if schema_info['has_depth']:
                node_id, domain, description, crawl_count, last_depth = row
                depth = last_depth if last_depth is not None else 0
            else:
                node_id, domain, description, crawl_count = row
                depth = 0  # Default for legacy databases

            # Add node with attributes
            G.add_node(
                node_id,
                domain=_safe_decode_text(domain),
                description=_safe_decode_text(description) if description else "",
                crawl_count=crawl_count or 1,
                depth=depth
            )

        # Load edges
        edge_rows = _load_edges_from_database(cursor)

        for from_id, to_id, weight in edge_rows:
            G.add_edge(from_id, to_id, weight=weight or 1)

    finally:
        conn.close()

    print(f"  ✅ Loaded: {G.number_of_nodes()} nodes, {G.number_of_edges()} edges")
    return G


def get_stats(G: nx.DiGraph) -> Dict[str, any]:
    """
    Get graph statistics.

    Args:
        G: NetworkX graph

    Returns:
        Dictionary with statistics
    """
    stats = {
        "nodes": G.number_of_nodes(),
        "edges": G.number_of_edges(),
        "density": nx.density(G),
        "avg_degree": sum(dict(G.degree()).values()) / G.number_of_nodes() if G.number_of_nodes() > 0 else 0,
    }

    # Find most connected nodes
    degrees = dict(G.degree())
    if degrees:
        top_nodes = sorted(degrees.items(), key=lambda x: x[1], reverse=True)[:5]
        stats["top_nodes"] = [
            (node_id, G.nodes[node_id].get("domain", "unknown"), degree)
            for node_id, degree in top_nodes
        ]

    return stats


def filter_by_weight(G: nx.DiGraph, min_weight: int) -> nx.DiGraph:
    """
    Remove edges below weight threshold.

    Args:
        G: NetworkX graph
        min_weight: Minimum edge weight to keep

    Returns:
        Filtered graph
    """
    edges_to_remove = [
        (u, v) for u, v, data in G.edges(data=True)
        if data.get("weight", 1) < min_weight
    ]

    G_filtered = G.copy()
    G_filtered.remove_edges_from(edges_to_remove)

    print(f"Filtered by weight >= {min_weight}: {G_filtered.number_of_edges()} edges remaining")
    return G_filtered


def remove_isolated(G: nx.DiGraph) -> nx.DiGraph:
    """
    Remove nodes with no edges (isolated nodes).

    Args:
        G: NetworkX graph

    Returns:
        Graph without isolated nodes
    """
    isolated = list(nx.isolates(G))
    G_filtered = G.copy()
    G_filtered.remove_nodes_from(isolated)

    print(f"Removed {len(isolated)} isolated nodes")
    return G_filtered


def extract_subgraph(G: nx.DiGraph, root_domain: str, depth: int) -> nx.DiGraph:
    """
    Extract N-hop neighborhood around a root domain.

    Args:
        G: NetworkX graph
        root_domain: Domain name to center on
        depth: Number of hops to include

    Returns:
        Subgraph centered on root domain
    """
    # Find node ID by domain name
    root_id = None
    for node_id, data in G.nodes(data=True):
        if data.get("domain") == root_domain:
            root_id = node_id
            break

    if root_id is None:
        raise ValueError(f"Domain '{root_domain}' not found in graph")

    # Get nodes within depth hops
    nodes = {root_id}
    for _ in range(depth):
        new_nodes = set()
        for node in nodes:
            new_nodes.update(G.successors(node))
            new_nodes.update(G.predecessors(node))
        nodes.update(new_nodes)

    # Extract subgraph
    subgraph = G.subgraph(nodes).copy()

    print(f"Extracted subgraph: {subgraph.number_of_nodes()} nodes, {subgraph.number_of_edges()} edges")
    return subgraph
