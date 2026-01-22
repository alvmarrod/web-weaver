#!/usr/bin/env python3
"""
Cosmograph.app Graph Exporter

Command-line tool to generate cosmograph.app-compatible CSV files from
web-weaver crawler results. Supports filtering, layout algorithms, and
statistical analysis.

Usage:
    python main.py --db crawler.db --output ./graph_data

The tool creates nodes.csv and edges.csv files that can be directly
imported into cosmograph.app for interactive network visualization.
"""

import sys
import argparse
from pathlib import Path

import loader
import renderer


def create_argument_parser() -> argparse.ArgumentParser:
    """
    Create and configure the command-line argument parser.

    Returns:
        Configured ArgumentParser instance
    """
    parser = argparse.ArgumentParser(
        description="Generate cosmograph.app compatible CSV files from crawler.db",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Basic CSV export for cosmograph.app
  python main.py --db crawler.db --output ./graph_data

  # Filter by edge weight and remove isolated nodes
  python main.py --db crawler.db --min-weight 3 --remove-isolated --output ./filtered_data

  # Extract subgraph around specific domain
  python main.py --db crawler.db --root xataka.com --depth 2 --output ./subgraph_data

  # Show statistics only (no file generation)
  python main.py --db crawler.db --stats

  # Use different layout algorithm
  python main.py --db crawler.db --layout spring --output ./spring_layout

Output:
  Creates nodes.csv and edges.csv in the specified output directory.
  These files can be directly imported into cosmograph.app for visualization.
        """
    )

    # Required arguments
    parser.add_argument(
        "--db",
        required=True,
        type=str,
        help="Path to crawler.db file"
    )

    # Output configuration
    parser.add_argument(
        "--output",
        default="./graph_data",
        type=str,
        help="Output directory for CSV files (default: ./graph_data)"
    )

    # Graph filters
    filter_group = parser.add_argument_group('filtering options')
    filter_group.add_argument(
        "--min-weight",
        type=int,
        help="Minimum edge weight threshold (removes weak connections)"
    )

    filter_group.add_argument(
        "--remove-isolated",
        action="store_true",
        help="Remove nodes with no connections"
    )

    filter_group.add_argument(
        "--root",
        type=str,
        help="Root domain for subgraph extraction"
    )

    filter_group.add_argument(
        "--depth",
        type=int,
        default=2,
        help="Depth for subgraph extraction (default: 2)"
    )

    # Layout and visualization
    viz_group = parser.add_argument_group('visualization options')
    viz_group.add_argument(
        "--layout",
        choices=["sfdp", "spring", "kamada_kawai"],
        default="sfdp",
        help="Graph layout algorithm (default: sfdp)"
    )

    # Analysis options
    analysis_group = parser.add_argument_group('analysis options')
    analysis_group.add_argument(
        "--stats",
        action="store_true",
        help="Show statistics only (no CSV generation)"
    )

    return parser


def validate_arguments(args: argparse.Namespace) -> None:
    """
    Validate command-line arguments and check file existence.

    Args:
        args: Parsed command-line arguments

    Raises:
        SystemExit: If validation fails
    """
    # Check database file exists
    db_path = Path(args.db)
    if not db_path.exists():
        print(f"❌ Error: Database file not found: {db_path}", file=sys.stderr)
        sys.exit(1)

    if not db_path.is_file():
        print(f"❌ Error: Path is not a file: {db_path}", file=sys.stderr)
        sys.exit(1)

    # Validate depth parameter
    if args.depth < 1:
        print(f"❌ Error: Depth must be positive, got: {args.depth}", file=sys.stderr)
        sys.exit(1)

    # Validate min-weight parameter
    if args.min_weight is not None and args.min_weight < 0:
        print(f"❌ Error: Min-weight must be non-negative, got: {args.min_weight}", file=sys.stderr)
        sys.exit(1)
def main() -> None:
    """
    Main entry point for the cosmograph.app graph exporter.

    Handles command-line parsing, graph loading, filtering, and CSV export.
    """
    # Parse and validate arguments
    parser = create_argument_parser()
    args = parser.parse_args()
    validate_arguments(args)

    # Load and process graph
    print(f"📂 Loading graph from {args.db}...")

    try:
        G = loader.load_graph(args.db)
    except Exception as e:
        print(f"❌ Error loading graph: {e}", file=sys.stderr)
        sys.exit(1)

    if G.number_of_nodes() == 0:
        print("⚠️  Warning: Graph is empty (no nodes found)", file=sys.stderr)
        sys.exit(1)

    # Apply filters in logical order
    original_nodes = G.number_of_nodes()
    original_edges = G.number_of_edges()

    if args.min_weight:
        print(f"🔍 Filtering edges with weight >= {args.min_weight}...")
        G = loader.filter_by_weight(G, args.min_weight)
        print(f"   Edges after weight filter: {G.number_of_edges()}")

    if args.remove_isolated:
        print(f"🔍 Removing isolated nodes...")
        G = loader.remove_isolated(G)
        print(f"   Nodes after isolation filter: {G.number_of_nodes()}")

    if args.root:
        print(f"🔍 Extracting subgraph around {args.root} (depth {args.depth})...")
        try:
            G = loader.extract_subgraph(G, args.root, args.depth)
            print(f"   Subgraph: {G.number_of_nodes()} nodes, {G.number_of_edges()} edges")
        except ValueError as e:
            print(f"❌ Error: {e}", file=sys.stderr)
            sys.exit(1)

    # Display statistics
    stats = loader.get_stats(G)

    print("\n" + "="*50)
    print("📊 GRAPH STATISTICS")
    print("="*50)
    print(f"🔗 Nodes: {stats['nodes']:,}")
    print(f"🔗 Edges: {stats['edges']:,}")
    print(f"🔗 Density: {stats['density']:.4f}")
    print(f"🔗 Average degree: {stats['avg_degree']:.2f}")

    if not args.stats and "top_nodes" in stats and stats["top_nodes"]:
        print(f"\n🏆 Top 5 most connected domains:")
        for i, (node_id, domain, degree) in enumerate(stats["top_nodes"], 1):
            print(f"   {i}. {domain}: {degree} connections")

    print("="*50)

    # Exit early if only showing statistics
    if args.stats:
        print("\n✅ Statistics complete!")
        return

    # Render CSV export for cosmograph.app
    print(f"\n🏗️ Generating CSV files for cosmograph.app...")
    try:
        renderer.render_cosmograph(G, args.output, layout=args.layout)
    except Exception as e:
        print(f"Error rendering graph: {e}", file=sys.stderr)
        sys.exit(1)

    print(f"\n✓ CSV files ready for cosmograph.app!")
    print(f"Import nodes.csv and edges.csv from {args.output} into cosmograph.app")
    print(f"You can also run the simulation after loading, instead of maintaining the loaded layout")


if __name__ == "__main__":
    main()
