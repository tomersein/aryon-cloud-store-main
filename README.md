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