import os
import requests
import json


def auth_headers() -> dict[str, str]:
    api_key = os.getenv("AMM_API_KEY", "")
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    return headers

def main():
    api_url = os.getenv("AMM_API_URL", "http://localhost:8080")
    project_id = os.getenv("AMM_PROJECT_ID", "unknown")
    
    payload = {
        "query": f"context for project: {project_id}",
        "opts": {
            "mode": "ambient",
            "limit": 10
        }
    }
    
    try:
        response = requests.post(f"{api_url}/v1/recall", json=payload, headers=auth_headers())
        response.raise_for_status()
        print(json.dumps(response.json(), indent=2))
    except Exception as e:
        print(f"Error during recall: {e}")

if __name__ == "__main__":
    main()
