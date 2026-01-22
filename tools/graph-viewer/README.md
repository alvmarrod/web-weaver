# Graph Viewer

Generate cosmograph.app-compatible CSV files from web-weaver crawler results for interactive network visualization.

## Overview

This tool converts SQLite crawler data into CSV files that can be directly imported into [cosmograph.app](https://cosmograph.app) for stunning interactive graph visualizations.

## Installation

```bash
pip install -r requirements.txt
```

**Requirements**: Python 3.10+, NetworkX, PyGraphviz (optional, for sfdp layout)

## Quick Start

```bash
# Generate CSV files for cosmograph.app
python main.py --db ../web-weaver/crawler.db --output ./graph_data

# Import nodes.csv and edges.csv into cosmograph.app
```

## Usage

### Basic Export

```bash
python main.py --db crawler.db --output ./my_graph
```

### With Filters

```bash
# Filter by minimum edge weight
python main.py --db crawler.db --min-weight 3 --output ./filtered_graph

# Remove isolated nodes
python main.py --db crawler.db --remove-isolated --output ./clean_graph

# Extract subgraph around domain
python main.py --db crawler.db --root xataka.com --depth 2 --output ./subgraph
```

### Statistics Only

```bash
python main.py --db crawler.db --stats
```

### Layout Options

```bash
# SFDP layout (default - best for large graphs)
python main.py --db crawler.db --layout sfdp --output ./graph_data

# Spring layout (organic clustering)
python main.py --db crawler.db --layout spring --output ./graph_data

# Kamada-Kawai layout (good for small graphs)
python main.py --db crawler.db --layout kamada_kawai --output ./graph_data
```

## Output

Generates two CSV files ready for cosmograph.app:

### `nodes.csv`

- **id**: Unique node identifier
- **label**: Domain name
- **x, y**: Computed coordinates from layout algorithm
- **size**: Based on node degree (number of connections)
- **color**: Hex color from green (few links) to red (many links)
- **cluster**: Grouped by crawling depth

### `edges.csv`

- **source, target**: Node IDs
- **width**: Relative weight based on number of links between entities
- **color**: Mapped from edge weight (weak/medium/strong/very_strong)

## Performance

- **Small graphs** (100-1K nodes): Instant
- **Medium graphs** (1K-5K nodes): ~5 seconds
- **Large graphs** (5K-10K nodes): ~30 seconds

For large graphs, use filters to improve processing time and visualization quality.

## Next Steps

1. Generate CSV files using this tool
2. Visit [cosmograph.app](https://cosmograph.app)
3. Upload your `nodes.csv` and `edges.csv` files
4. Explore your network with professional interactive visualization
