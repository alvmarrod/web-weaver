"""
Cosmograph.app CSV Export Module

This module provides functionality to export NetworkX graphs to CSV files
compatible with cosmograph.app for interactive network visualization.

The module handles:
- Graph layout computation (SFDP, Spring, Kamada-Kawai)
- Node positioning and normalization
- Color mapping based on connectivity
- Relative edge weight calculation
- CSV export with proper formatting
"""

import csv
import math
import random
from pathlib import Path
from typing import Dict, Tuple
from collections import defaultdict

import networkx as nx


# =============================================================================
# Layout Algorithms
# =============================================================================

def compute_layout(G: nx.Graph, layout: str = "sfdp") -> Dict[int, Tuple[float, float]]:
    """
    Compute node positions using the specified layout algorithm.

    Args:
        G: NetworkX graph to layout
        layout: Layout algorithm to use. Options:
            - 'sfdp': Scalable Force-Directed Placement (requires pygraphviz)
            - 'spring': Spring-force model with NetworkX
            - 'kamada_kawai': Kamada-Kawai layout algorithm

    Returns:
        Dictionary mapping node_id -> (x, y) coordinates

    Raises:
        ImportError: If pygraphviz is not installed for sfdp layout
        ValueError: If unsupported layout algorithm is specified
    """
    print(f"💻 Computing layout: {layout}")

    layout_functions = {
        "sfdp": _compute_sfdp_layout,
        "spring": _compute_spring_layout,
        "kamada_kawai": _compute_kamada_kawai_layout
    }

    if layout not in layout_functions:
        raise ValueError(f"Unsupported layout: {layout}. Choose from: {list(layout_functions.keys())}")

    return layout_functions[layout](G)


def _compute_sfdp_layout(G: nx.Graph) -> Dict[int, Tuple[float, float]]:
    """Compute SFDP layout using pygraphviz."""
    try:
        import pygraphviz  # noqa
    except ImportError:
        raise ImportError(
            "PyGraphviz is required for sfdp layout. Install with `pip install pygraphviz`"
        )

    return nx.nx_agraph.graphviz_layout(
        G,
        prog="sfdp",
        args="-Goverlap=prism -Grepulsiveforce=2"
    )


def _compute_spring_layout(G: nx.Graph) -> Dict[int, Tuple[float, float]]:
    """Compute spring layout using NetworkX."""
    return nx.spring_layout(G, k=0.5, iterations=50)


def _compute_kamada_kawai_layout(G: nx.Graph) -> Dict[int, Tuple[float, float]]:
    """Compute Kamada-Kawai layout using NetworkX."""
    return nx.kamada_kawai_layout(G)

def normalize_positions(
    pos: Dict[int, Tuple[float, float]],
    scale: float = 10.0,
    jitter: float = 0.0
) -> Dict[int, Tuple[float, float]]:
    """
    Normalize node positions to fit within a specified scale.

    Args:
        pos: Dictionary mapping node_id -> (x, y) coordinates
        scale: Target scale for normalized coordinates
        jitter: Random jitter amount to add (0.0 = no jitter)

    Returns:
        Dictionary with normalized coordinates
    """
    if not pos:
        return {}

    # Extract all coordinates
    coordinates = list(pos.values())
    xs = [coord[0] for coord in coordinates]
    ys = [coord[1] for coord in coordinates]

    # Calculate bounds
    min_x, max_x = min(xs), max(xs)
    min_y, max_y = min(ys), max(ys)

    # Avoid division by zero
    range_x = max_x - min_x if max_x != min_x else 1.0
    range_y = max_y - min_y if max_y != min_y else 1.0

    # Normalize positions
    normalized = {}
    for node_id, (x, y) in pos.items():
        norm_x = (x - min_x) / range_x * scale
        norm_y = (y - min_y) / range_y * scale

        # Add jitter if specified
        if jitter > 0:
            norm_x += random.uniform(-jitter, jitter)
            norm_y += random.uniform(-jitter, jitter)

        normalized[node_id] = (norm_x, norm_y)

    return normalized


# =============================================================================
# Visual Encoding Functions
# =============================================================================

def calculate_node_size(degree: int, base_size: float = 1.0, scale_factor: float = 0.3, max_size: float = 5.0) -> float:
    """
    Calculate node size based on degree (number of connections).

    Uses logarithmic scaling to handle wide range of degree values:
    - Nodes with few connections get smaller sizes
    - Nodes with many connections get larger sizes
    - Maximum size is capped to avoid visual dominance

    Args:
        degree: Number of connections for this node
        base_size: Minimum size for nodes with degree 1
        scale_factor: How much size increases with degree
        max_size: Maximum allowed size

    Returns:
        Calculated size value for cosmograph.app
    """
    if degree <= 1:
        return base_size

    size = base_size * (1 + scale_factor * math.log2(degree))
    return min(size, max_size)


def calculate_node_color(degree: int) -> str:
    """
    Generate hex color based on node degree (connectivity).

    Creates a gradient from green (few connections) to red (many connections):
    - Green (#00ff00): Low connectivity (degree 1-2)
    - Yellow (#ffff00): Medium connectivity
    - Orange (#ff8000): High connectivity
    - Red (#ff0000): Very high connectivity

    Args:
        degree: Number of connections for this node

    Returns:
        Hex color string for cosmograph.app
    """
    # Normalize degree using log scale (most nodes have degree 1-10, outliers 50+)
    normalized = min(math.log2(degree + 1) / 6.0, 1.0)

    # Create smooth gradient through color space
    if normalized <= 0.33:
        # Green to Yellow transition
        factor = normalized / 0.33
        red = int(factor * 255)
        green = 255
        blue = 0
    elif normalized <= 0.66:
        # Yellow to Orange transition
        factor = (normalized - 0.33) / 0.33
        red = 255
        green = int(255 * (1 - factor * 0.5))  # 255 → 128
        blue = 0
    else:
        # Orange to Red transition
        factor = (normalized - 0.66) / 0.34
        red = 255
        green = int(128 * (1 - factor))  # 128 → 0
        blue = 0

    return f"#{red:02x}{green:02x}{blue:02x}"


# =============================================================================
# Edge Processing
# =============================================================================

def calculate_edge_weights(G: nx.Graph) -> Dict[Tuple[str, str], float]:
    """
    Calculate relative edge weights based on connection frequency.

    Analyzes the graph to count how many edges exist between each pair
    of nodes, then normalizes these counts to create relative weights
    suitable for cosmograph.app's width property.

    Args:
        G: NetworkX graph to analyze

    Returns:
        Dictionary mapping (source, target) tuples to relative weights
    """
    print("💻 Calculating relative edge weights...")

    # Count edges between each pair of nodes
    edge_counts = defaultdict(int)
    for source, target in G.edges():
        # Normalize edge direction for consistent counting
        edge_key = tuple(sorted([str(source), str(target)]))
        edge_counts[edge_key] += 1

    if not edge_counts:
        return {}

    # Calculate relative weights (normalize to reasonable range)
    max_count = max(edge_counts.values())
    min_width, max_width = 0.1, 3.0

    relative_weights = {}
    for edge_key, count in edge_counts.items():
        # Linear scaling from min_width to max_width
        normalized_weight = min_width + (count / max_count) * (max_width - min_width)
        relative_weights[edge_key] = round(normalized_weight, 2)

    return relative_weights


def categorize_edge_strength(weight: int) -> str:
    """
    Categorize edge weight into strength levels for color mapping.

    Args:
        weight: Original edge weight from database

    Returns:
        Strength category string
    """
    if weight <= 1:
        return "weak"
    elif weight <= 5:
        return "medium"
    elif weight <= 10:
        return "strong"
    else:
        return "very_strong"


# =============================================================================
# Data Sanitization Functions
# =============================================================================

def sanitize_domain_name(domain: str) -> str:
    """
    Sanitize domain name to fix common data corruption issues.

    Handles:
    - Commas that should be dots (e.g., "domain,com" -> "domain.com")
    - Extra whitespace
    - Invalid characters in domain names

    Args:
        domain: Raw domain name from database

    Returns:
        Cleaned domain name
    """
    if not domain:
        return "unknown"

    # Convert to string and strip whitespace
    cleaned = str(domain).strip()

    # Fix common corruption: comma instead of dot
    # Only replace comma with dot if it looks like a domain
    if ',' in cleaned and '.' not in cleaned:
        # Check if it looks like a domain with comma instead of dot
        parts = cleaned.split(',')
        if len(parts) >= 2 and all(part.isalnum() or '-' in part for part in parts):
            cleaned = '.'.join(parts)
            print(f"  ⚠️  Fixed corrupted domain: {domain} -> {cleaned}")

    # Remove any remaining problematic characters for CSV
    # Keep only valid domain characters: letters, numbers, dots, hyphens
    import re
    cleaned = re.sub(r'[^a-zA-Z0-9.-]', '', cleaned)

    # Ensure it's not empty after cleaning
    if not cleaned or cleaned in ['.', '-']:
        cleaned = f"invalid_domain_{hash(domain) % 10000}"
        print(f"  ⚠️  Invalid domain replaced: {domain} -> {cleaned}")

    return cleaned


def sanitize_csv_field(value: str) -> str:
    """
    Sanitize a field value for CSV export to prevent quote escaping issues.

    Handles:
    - Removes or escapes problematic characters
    - Prevents quote doubling issues
    - Ensures clean CSV output

    Args:
        value: Raw field value

    Returns:
        Sanitized value safe for CSV
    """
    if not value:
        return ""

    # Convert to string and strip whitespace
    cleaned = str(value).strip()

    # Remove or replace problematic characters
    # Remove extra quotes that could cause CSV escaping issues
    cleaned = cleaned.replace('"', '').replace("'", "")

    # Remove newlines and carriage returns
    cleaned = cleaned.replace('\n', ' ').replace('\r', ' ')

    # Remove excessive whitespace
    import re
    cleaned = re.sub(r'\s+', ' ', cleaned)

    return cleaned.strip()


# =============================================================================
# CSV Export Functions
# =============================================================================

def export_nodes_to_csv(
    G: nx.Graph,
    positions: Dict[int, Tuple[float, float]],
    output_path: str
) -> None:
    """
    Export graph nodes to CSV format compatible with cosmograph.app.

    Creates a nodes.csv file with all necessary columns for visualization:
    - id: Unique node identifier
    - label: Human-readable domain name (sanitized)
    - x, y: Computed layout coordinates
    - size: Visual size based on connectivity
    - color: Hex color representing connectivity level
    - cluster: Grouping by crawl depth
    - Additional metadata: crawl_count, depth

    Args:
        G: NetworkX graph containing node data
        positions: Dictionary mapping node_id -> (x, y) coordinates
        output_path: Directory path for output files
    """
    print("💻 Exporting nodes to CSV...")

    output_dir = Path(output_path)
    csv_path = output_dir / "nodes.csv"

    fieldnames = ['id', 'label', 'x', 'y', 'size', 'color', 'cluster', 'crawl_count', 'depth']

    # Track any data issues for reporting
    sanitization_count = 0

    with open(csv_path, 'w', newline='', encoding='utf-8') as csvfile:
        writer = csv.DictWriter(
            csvfile,
            fieldnames=fieldnames,
            quoting=csv.QUOTE_MINIMAL,  # Only quote when necessary
            escapechar='\\',  # Use backslash for escaping
            lineterminator='\n'  # Consistent line endings
        )
        writer.writeheader()

        for node_id in G.nodes():
            node_data = G.nodes[node_id]

            # Extract and sanitize node properties
            raw_domain = node_data.get("domain", str(node_id))
            sanitized_domain = sanitize_domain_name(raw_domain)

            if sanitized_domain != raw_domain:
                sanitization_count += 1

            # Calculate visual properties
            degree = G.degree(node_id)
            crawl_count = node_data.get("crawl_count", 1)
            depth = node_data.get("depth", 0)
            x, y = positions[node_id]

            size = calculate_node_size(degree)
            color = calculate_node_color(degree)
            cluster = f"depth_{depth}"

            # Additional sanitization for any string fields
            cluster = sanitize_csv_field(cluster)

            writer.writerow({
                'id': str(node_id),
                'label': sanitized_domain,  # Already sanitized
                'x': round(x, 3),
                'y': round(y, 3),
                'size': round(size, 3),
                'color': color,
                'cluster': cluster,
                'crawl_count': crawl_count,
                'depth': depth
            })

    print(f"  ✅ Nodes exported → {csv_path}")
    if sanitization_count > 0:
        print(f"  ⚠️  Sanitized {sanitization_count} domain names with data issues")


def export_edges_to_csv(G: nx.Graph, output_path: str) -> None:
    """
    Export graph edges to CSV format compatible with cosmograph.app.

    Creates an edges.csv file with columns:
    - source, target: Node identifiers (sanitized)
    - width: Relative visual thickness based on connection frequency
    - color: Strength category for styling
    - weight: Original edge weight from database

    Args:
        G: NetworkX graph containing edge data
        output_path: Directory path for output files
    """
    print("💻 Exporting edges to CSV...")

    output_dir = Path(output_path)
    csv_path = output_dir / "edges.csv"

    # Pre-calculate relative weights for all edges
    edge_weights = calculate_edge_weights(G)

    fieldnames = ['source', 'target', 'width', 'color', 'weight']

    with open(csv_path, 'w', newline='', encoding='utf-8') as csvfile:
        writer = csv.DictWriter(
            csvfile,
            fieldnames=fieldnames,
            quoting=csv.QUOTE_MINIMAL,  # Only quote when necessary
            escapechar='\\',  # Use backslash for escaping
            lineterminator='\n'  # Consistent line endings
        )
        writer.writeheader()

        for source, target in G.edges():
            edge_data = G.edges[source, target]
            original_weight = edge_data.get('weight', 1)

            # Get relative width based on connection frequency
            edge_key = tuple(sorted([str(source), str(target)]))
            relative_width = edge_weights.get(edge_key, 0.1)

            # Categorize strength for color mapping
            strength_category = categorize_edge_strength(original_weight)

            writer.writerow({
                'source': str(source),
                'target': str(target),
                'width': relative_width,
                'color': strength_category,
                'weight': original_weight
            })

    print(f"  ✅ Edges exported → {csv_path}")


# =============================================================================
# Public API
# =============================================================================

def render_cosmograph_csv(
    G: nx.DiGraph,
    output_path: str,
    layout: str = "sfdp",
    scale: float = 50.0,
    jitter: float = 0.05
) -> None:
    """
    Export NetworkX graph to cosmograph.app-compatible CSV files.

    This is the main entry point for generating visualization-ready data.
    Creates two files in the specified directory:
    - nodes.csv: Node data with positions, sizes, colors, and metadata
    - edges.csv: Edge data with relative weights and styling information

    Args:
        G: NetworkX directed graph to export
        output_path: Output directory for CSV files
        layout: Layout algorithm ('sfdp', 'spring', 'kamada_kawai')
        scale: Coordinate scaling factor for layout
        jitter: Random jitter amount for position variation

    Raises:
        ImportError: If required layout dependencies are missing
        ValueError: If unsupported layout algorithm is specified
        OSError: If output directory cannot be created
    """
    print(f"🚀 Starting cosmograph.app CSV export...")

    # Ensure output directory exists
    output_dir = Path(output_path)
    output_dir.mkdir(parents=True, exist_ok=True)

    # Compute and normalize node positions
    print(f"📐 Computing {layout} layout for {G.number_of_nodes()} nodes...")
    positions = compute_layout(G, layout)
    normalized_positions = normalize_positions(positions, scale=scale, jitter=jitter)

    # Export data files
    export_nodes_to_csv(G, normalized_positions, output_path)
    export_edges_to_csv(G, output_path)

    print("✅ Export completed successfully!")
    print(f"📁 Files ready in: {output_dir.absolute()}")
    print("🌐 Import nodes.csv and edges.csv into cosmograph.app")


# Maintain backward compatibility
render_cosmograph = render_cosmograph_csv
render_interactive = render_cosmograph_csv
