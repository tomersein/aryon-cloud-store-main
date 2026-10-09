# CloudStore
## Description
Cloud environments typically have some hierarchy which is a recursive structure.

Our goal is to store a cloud hierarchy in a postgres database and provide an endpoint to fetch a cloud hierarchy and an endpoint to store a cloud hierarchy - storing a cloud hierarchy is an "upsert" operation.

Cloud hierarchies tend to change, and we need to deal with the following cases:
- a new node in the hierarchy was added
- an existing node was removed
- an existing node was moved to a different parent

## Requirements
### Database Modeling
The goal of the exercise is to store a cloud hierarchy in a way that is efficient at large scale.

You need to design a schema and a storage strategy that will allow you to store and retrieve a cloud hierarchy with minimal operations on the database.

Also keep in mind that data integrity is important, so you need to make sure that the hierarchy is always correct based on the last update.

### Endpoints
- POST /hierarchy
  - Request body: JSON object representing a cloud hierarchy
  - Response: 200 OK


- GET /hierarchy/{node_id}
  - Response: JSON object representing a cloud hierarchy starting from the node with the given id

### Server
You are given a docker-compose application with a running Python server (on port 8081) and a running Go server (running on port 8080), you may implement either of them.

Each server is already exposing 1 endpoint of some mock table so you have a running reference.

### Database
You are given a running postgres database with 1 mock table for reference.

The database is initialized with the `init.sql` file, there you can add your additional tables.

## Examples
An example of a cloud hierarchy is:
```
{
  "id": 1,
  "type": "management_group",
  "children": [
    {
      "id": 2,
      "type": "management_group",
      "children": []
    },
    {
      "id": 3,
      "type": "subscription",
      "children": [
        {
          "id": 4,
          "type": "subscription",
          "children": [
            {
              "id": 5,
              "type": "resource_group",
              "children": []
            }
          ]
        }
      ]
    },
    {
      "id": 6,
      "type": "subscription",
      "children": [
        {
          "id": 7,
          "type": "resource_group",
          "children": []
        }
      ]
    }
  ]
}
```

### Testing

You can find the "tests" folder that contains 2 main things:
- "objects" folder containing some sample hierarchies for testing
- "run_tests.py" script that will run some tests, it will store each hierarchy and then fetch it to compare the results.

You can add tests as you see fit. (but you can't delete existing tests)
---

# Solution

Implemented in the **Go server** (`go-server/`, port 8080). The Python server is left as provided, with its dependencies upgraded to clear known vulnerabilities.

## Running it

```bash
docker compose up -d --build
python tests/run_tests.py          # the provided tests
python tests/run_extra_tests.py    # edge cases: moves, removals, ordering, concurrency, 10k-node tree
cd go-server && go test -race ./...  # unit tests, no database needed
```

## API

| Request | Success | Errors |
|---|---|---|
| `POST /hierarchy` with a tree | `200 {"id": <root>, "nodes": <count>}` | `400` malformed JSON, missing `id`/`type`, unknown `type`, duplicate id · `409` the root would end up under its own descendant · `413` body over 32 MB (about 500,000 nodes) |
| `GET /hierarchy/{id}` | `200` the subtree under that node | `400` id is not an integer · `404` unknown node |

Unexpected failures return `500 {"error": "internal server error"}`. The cause is logged on the server, never sent to the client.

## Data model

One table, an **adjacency list**:

```sql
nodes(id BIGINT PRIMARY KEY, type TEXT CHECK (...), parent_id BIGINT REFERENCES nodes(id), position INT)
INDEX (parent_id, position)
```

`position` is the node's index among its siblings, so children come back in the order they were sent.

Why an adjacency list: the requirements stress **moves**, and this is the only common tree encoding where moving a node, subtree included, is a single-row update.

| Encoding | Read a subtree | Move a subtree | |
|---|---|---|---|
| **Adjacency list + recursive CTE** | 1 query, index scan per level | update 1 row | chosen |
| Closure table | 1 query, no recursion | rewrite (subtree × depth) rows | moves get expensive as trees deepen |
| Materialized path / `ltree` | 1 indexed query | rewrite every path in the subtree | same problem, one row per descendant |
| Nested sets | 1 range query | renumber a large part of the tree | built for read-mostly trees |

## Semantics

- **A POST is the complete truth for the subtree under its root.** Nodes in the request are inserted or updated, matched by id. Nodes previously under the root but absent from the request are removed, along with their descendants.
- **Moves are implicit.** A node whose parent differs from the stored one is re-parented, including across trees. The last write wins.
- **The posted root keeps its existing parent**, so posting a subtree updates it in place rather than detaching it. Posting a root whose current ancestor appears inside the request would create a loop, and is rejected with `409`.
- **Ids are global**, so a node removed earlier can come back anywhere, as node 7 does in `tests/objects/6.json`.
- **Removal is a hard delete.** The task asks only for the latest state. If history were needed, I'd add a separate history table rather than soft-deleting rows in `nodes`, which would make every tree query filter out tombstones.
- **Type nesting is not restricted.** The provided data nests a subscription under a subscription and a resource group under a resource group. Only the three known type names are accepted.

## Database operations per request

**POST: one transaction, 4 statements, whatever the size of the tree.**

1. `pg_advisory_xact_lock`: serializes writers.
2. A recursive walk up from the root to check for loops.
3. One bulk upsert: the tree is flattened in Go into column arrays and written with `INSERT … SELECT FROM unnest(...) ON CONFLICT (id) DO UPDATE`. Rows that did not change are skipped by a `WHERE … IS DISTINCT FROM`, so re-posting an unchanged tree writes nothing.
4. One `DELETE` of nodes still under the root that the request did not mention.

The request is fully validated before the transaction starts, and any failure rolls back, so a rejected POST changes nothing.

**GET: one recursive query**, ordered by depth, then parent, then position. Go assembles the tree in a single pass because every parent arrives before its children. Being a single statement, it reads a consistent snapshot even while a POST is running.

The 10,101-node tree in `run_extra_tests.py` stores in about 70 ms and reads in about 20 ms on a laptop.

## Integrity and concurrency

- **In the database:** the foreign key prevents dangling parents, and the `CHECK` constraint on `type` rejects unknown types.
- **In Go, before writing:** duplicate ids and malformed nodes are rejected.
- **In the transaction:** the loop check runs before anything is written.
- **Writers are serialized** with a transaction-scoped advisory lock. Readers are never blocked (MVCC). Each POST is only a handful of set-based statements, so a single writer lane is cheap.
  - Locking per tree would allow parallel writes, but a POST can move nodes in from any other tree, so the lock set isn't known up front. That's a reasonable next step only if write throughput becomes the bottleneck.
  - `run_extra_tests.py` fires 20 concurrent POSTs at one tree and checks that the result is exactly one of the posted versions.

## Code layout (`go-server/`)

```
main.go                      configuration, database connection, wiring
internal/
├── handler/                 HTTP: parse, validate, map errors to status codes
├── store/                   all SQL: the Postgres implementation of handler.Store
└── hierarchy/               the tree model, Flatten / BuildTree, domain errors
```

Dependencies point one way: `handler` and `store` both import `hierarchy`, and never each other. The handler depends only on the `Store` interface, so its unit tests use a fake store and need no database. `store` is covered by the integration tests against real Postgres.

The SQL is handwritten and fully parameterized. The recursive CTEs, the `unnest` bulk upsert and the advisory lock are beyond what Go ORMs express. An ORM would also pull the code toward one query per node.

## CI (`.github/workflows/ci.yml`)

| Job | What it runs |
|---|---|
| lint | golangci-lint, including `gosec`, `sqlclosecheck` and `rowserrcheck` |
| unit-tests | `go test -race` with a coverage summary |
| integration-tests | `docker compose` stack, then the provided tests and the edge-case tests |
| vulnerabilities | `govulncheck` for reachable Go vulnerabilities; Grype over repository dependencies and over the built server image, failing on High or above |
| zanadir | CI coverage report in the job summary |

The server image is multi-stage: a static binary on distroless, non-root, about 26 MB.

## What I'd do next

- Use [sqlc](https://sqlc.dev) to generate type-safe Go from the `.sql` queries.
- Add a depth or size limit on GET, for very large trees.
- Move to the `pgx` driver, whose `COPY` support would speed up very large writes.
- Lock per tree if write throughput becomes the bottleneck.
