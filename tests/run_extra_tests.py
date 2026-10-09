"""Edge-case tests for the hierarchy API, complementing run_tests.py.

Every test uses its own id range (900000 and up) and posts the full state it
starts from, so the suite can run against any database, in any order, repeatedly.
Exits non-zero when a test fails.
"""
import os
import sys
import threading
import time

import requests

BASE_URL = f"http://{os.getenv('HOST', 'localhost')}:{os.getenv('PORT', '8080')}/hierarchy"


def node(node_id, node_type="subscription", children=None):
    return {"id": node_id, "type": node_type, "children": children or []}


def post(tree):
    return requests.post(BASE_URL, json=tree)


def post_raw(body):
    return requests.post(BASE_URL, data=body, headers={"Content-Type": "application/json"})


def get(node_id):
    return requests.get(f"{BASE_URL}/{node_id}")


def store(tree):
    response = post(tree)
    assert response.status_code == 200, f"POST failed: {response.status_code} {response.text}"


def assert_tree(node_id, expected):
    response = get(node_id)
    assert response.status_code == 200, f"GET {node_id}: {response.status_code} {response.text}"
    assert response.json() == expected, f"GET {node_id} returned {response.json()}, expected {expected}"


def test_get_returns_the_subtree_of_a_non_root_node():
    leaf = node(900002, "resource_group")
    store(node(900000, "management_group", [node(900001, children=[leaf])]))
    assert_tree(900001, node(900001, children=[leaf]))


def test_get_rejects_unknown_and_malformed_ids():
    assert get(999999999).status_code == 404
    assert get("abc").status_code == 400


def test_children_keep_the_order_they_were_sent_in():
    root = node(901000, "management_group", [node(901003), node(901001), node(901002)])
    store(root)
    assert_tree(901000, root)

    reordered = node(901000, "management_group", [node(901002), node(901003), node(901001)])
    store(reordered)
    assert_tree(901000, reordered)


def test_removing_a_node_removes_its_descendants():
    store(node(902000, "management_group", [node(902001, children=[node(902002, "resource_group")])]))
    store(node(902000, "management_group"))
    assert_tree(902000, node(902000, "management_group"))
    assert get(902001).status_code == 404
    assert get(902002).status_code == 404


def test_node_moves_between_trees():
    store(node(903000, "management_group", [node(903001, children=[node(903002, "resource_group")])]))
    store(node(904000, "management_group"))

    moved = node(903001, children=[node(903002, "resource_group")])
    store(node(904000, "management_group", [moved]))

    assert_tree(903000, node(903000, "management_group"))
    assert_tree(904000, node(904000, "management_group", [moved]))


def test_posting_a_subtree_keeps_it_attached_to_its_parent():
    store(node(905000, "management_group", [node(905001, children=[node(905002, "resource_group")])]))
    store(node(905001, children=[node(905003, "resource_group")]))

    assert_tree(905000, node(905000, "management_group", [node(905001, children=[node(905003, "resource_group")])]))
    assert get(905002).status_code == 404


def test_type_changes_are_stored():
    store(node(906000, "management_group", [node(906001, "subscription")]))
    store(node(906000, "management_group", [node(906001, "resource_group")]))
    assert_tree(906000, node(906000, "management_group", [node(906001, "resource_group")]))


def test_invalid_posts_are_rejected_and_change_nothing():
    original = node(907000, "management_group", [node(907001)])
    store(original)

    invalid_bodies = {
        "broken JSON": '{"id": 907000,',
        "missing id": '{"type": "subscription", "children": []}',
        "missing type": '{"id": 907000, "children": []}',
        "unknown type": '{"id": 907000, "type": "virtual_machine", "children": []}',
        "duplicate id": '{"id": 907000, "type": "management_group", "children": [{"id": 907000, "type": "subscription"}]}',
    }
    for case, body in invalid_bodies.items():
        response = post_raw(body)
        assert response.status_code == 400, f"{case}: expected 400, got {response.status_code}"
    assert_tree(907000, original)


def test_oversized_bodies_are_rejected():
    body = " " * (32 * 1024 * 1024) + '{"id": 911000, "type": "subscription", "children": []}'
    response = post_raw(body)
    assert response.status_code == 413, f"expected 413, got {response.status_code}"
    assert get(911000).status_code == 404


def test_a_root_cannot_be_moved_under_its_own_descendant():
    original = node(908000, "management_group", [node(908001)])
    store(original)

    response = post(node(908001, children=[node(908000, "management_group")]))
    assert response.status_code == 409, f"expected 409, got {response.status_code}"
    assert_tree(908000, original)


def test_reposting_the_same_tree_is_idempotent():
    tree = node(909000, "management_group", [node(909001, children=[node(909002, "resource_group")])])
    store(tree)
    store(tree)
    assert_tree(909000, tree)


def test_concurrent_posts_leave_one_complete_version():
    shape_a = node(910000, "management_group", [node(910001), node(910002)])
    shape_b = node(910000, "management_group", [node(910003, children=[node(910001, "resource_group")])])
    statuses = []

    def worker(tree):
        statuses.append(post(tree).status_code)

    threads = [threading.Thread(target=worker, args=(shape_a if i % 2 else shape_b,)) for i in range(20)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    assert statuses == [200] * 20, f"unexpected statuses: {statuses}"
    final = get(910000).json()
    assert final in (shape_a, shape_b), f"tree is a mix of both versions: {final}"


def test_large_tree_round_trip():
    # 1 root, 100 subscriptions, 100 resource groups each: 10,101 nodes.
    tree = node(1_000_000, "management_group", [
        node(1_000_000 + s * 1000, children=[
            node(1_000_000 + s * 1000 + r, "resource_group") for r in range(1, 101)
        ]) for s in range(1, 101)
    ])

    started = time.monotonic()
    store(tree)
    stored = time.monotonic()
    assert_tree(1_000_000, tree)
    fetched = time.monotonic()
    print(f"    10,101 nodes: POST {stored - started:.2f}s, GET {fetched - stored:.2f}s")


def main():
    tests = [(name, fn) for name, fn in globals().items() if name.startswith("test_") and callable(fn)]
    failures = 0
    for name, fn in tests:
        try:
            fn()
            print(f"✅ {name}")
        except Exception as e:
            failures += 1
            print(f"❌ {name}: {e}")

    print(f"\n{len(tests) - failures}/{len(tests)} passed")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
