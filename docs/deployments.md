# Deployments

## Deploying a project

```
POST /projects/:projectName/deployments
```

**POST Multipart Form**

| Key     | Type                            | Required? | Description                                         |
| ------- | ------------------------------- | --------- | --------------------------------------------------- |
| payload | file (application/octet-stream) | Required  | bundle tarball containing all assets to be deployed |

* `Content-Length` header is required.
* Must be a multipart POST request, not the regular form-data POST request

**Possible responses**

* **202** - Deployment accepted
  Example:
  ```json
  {
    "deployment": {
      "id": 123,
      "state": "uploaded"
    }
  }
  ```

* **422** - Invalid params
  Example:
  ```json
  {
    "error": "invalid_params",
    "errors": {
      "payload": "is required"
    }
  }
  ```

* **400** - Invalid request
  ```json
  {
    "error": "invalid_request",
    "errors": {
      "name": "request body is too large"
    }
  }
  ```
