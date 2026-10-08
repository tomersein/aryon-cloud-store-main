import json
import requests
from typing import Dict, Any
from pathlib import Path


host = 'localhost'
port = '8080'
endpoint = 'hierarchy'


class CloudHierarchyTest:
    def __init__(self):
        self.base_url = f"http://{host}:{port}"
        self.store_endpoint = f"{self.base_url}/{endpoint}"
        self.fetch_endpoint = f"{self.base_url}/{endpoint}"

    def store_hierarchy(self, hierarchy: Dict[str, Any]) -> requests.Response:
        """
        Store a cloud hierarchy object.

        Args:
            hierarchy: The cloud hierarchy object to store

        Returns:
            requests.Response object
        """
        try:
            response = requests.post(self.store_endpoint, json=hierarchy)
            response.raise_for_status()
            return response
        except requests.exceptions.RequestException as e:
            raise Exception(f"Failed to store hierarchy: {str(e)}")

    def fetch_hierarchy(self, hierarchy_id: int) -> Dict[str, Any]:
        """
        Fetch a cloud hierarchy by its root ID.

        Args:
            hierarchy_id: The ID of the root node

        Returns:
            The retrieved hierarchy object
        """
        try:
            response = requests.get(f"{self.fetch_endpoint}/{hierarchy_id}")
            response.raise_for_status()
            return response.json()
        except requests.exceptions.RequestException as e:
            raise Exception(f"Failed to fetch hierarchy: {str(e)}")

    def compare_hierarchies(self, original: Dict[str, Any], retrieved: Dict[str, Any]) -> bool:
        """
        Compare two hierarchy objects for equality.

        Args:
            original: The original hierarchy object
            retrieved: The retrieved hierarchy object

        Returns:
            True if hierarchies are equal, False otherwise
        """
        return json.dumps(original, sort_keys=True) == json.dumps(retrieved, sort_keys=True)


def run_tests() -> None:
    """
    Run tests for all JSON files in the test_data directory.
    """
    tester = CloudHierarchyTest()
    test_files = sorted(
        Path('tests/objects').absolute().glob("*.json"),
        key=lambda x: int(x.stem)  # stem gets filename without extension
    )
    total_tests = 0
    passed_tests = 0

    for test_file in test_files:
        total_tests += 1
        print(f"\nTesting file: {test_file.name}")

        try:
            # Load test data
            with open(test_file, 'r') as f:
                original_hierarchy = json.load(f)

            # Store hierarchy
            store_response = tester.store_hierarchy(original_hierarchy)
            print(f"Store response status: {store_response.status_code}")

            # Fetch hierarchy
            root_id = original_hierarchy['id']
            retrieved_hierarchy = tester.fetch_hierarchy(root_id)

            # Compare hierarchies
            if tester.compare_hierarchies(original_hierarchy, retrieved_hierarchy):
                print("✅ Test passed: Hierarchies match")
                passed_tests += 1
            else:
                print("❌ Test failed: Hierarchies don't match")
                print("Original:", json.dumps(original_hierarchy, indent=2))
                print("Retrieved:", json.dumps(retrieved_hierarchy, indent=2))

        except Exception as e:
            print(f"❌ Test failed with error: {str(e)}")


if __name__ == "__main__":
    run_tests()