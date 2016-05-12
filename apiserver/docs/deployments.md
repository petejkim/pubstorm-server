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
  * Example:
  ```json
  {
    "deployment": {
      "id": 123,
      "state": "uploaded",
      "version": 1
    }
  }
  ```

* **422** - Invalid params
  * Example:
  ```json
  {
    "error": "invalid_params",
    "errors": {
      "payload": "is required"
    }
  }
  ```

* **400** - Invalid request
  * Example:
  ```json
  {
    "error": "invalid_request",
    "errors": {
      "name": "request body is too large"
    }
  }
  ```

## Fetching a deployment

```
GET /projects/:projectName/deployments/:id
```

**Possible responses**

* **200** - Deployment fetched
  * Example:
  ```json
  {
    "deployment": {
      "id": 123,
      "state": "deployed",
      "version": 1,
      "deployed_at": "2016-04-23T18:25:43.511Z"
    }
  }
  ```

* **404** - Project not found
  * Example:
  ```json
  {
    "error": "not_found",
    "error_description": "project could not be found"
  }
  ```

## Rolling back to a deployment

```
POST /projects/:projectName/rollback
```

**POST Form Params**

| Key     | Type | Required? | Description                  |
| --------| ---- | --------- | -----------------------------|
| version | int  | Required  | verision to rollback to      |


**Possible responses**

* **202** - Rollback accepted
  * Example:
  ```json
  {
    "deployment": {
      "id": 123,
      "state": "pending_rollback",
      "version": 1,
      "deployed_at": "2016-04-23T18:25:43.511Z"
    }
  }
  ```

* **404** - Project not found
  * Example:
  ```json
  {
    "error": "not_found",
    "error_description": "project could not be found"
  }
  ```

* **422** - Deployment not found
  * Example:
  ```json
  {
    "error":             "invalid_request",
    "error_description": "complete deployment with given id could not be found"
  }
  ```

* **422** - Deployment is already active
  * Example:
  ```json
  {
    "error": "invalid_request",
    "error_description": "the specified deployment is already active"
  }
  ```

## Fetch list of completed deployments

```
GET /projects/:projectName/deployments
```

**Possible responses**

* **200** - Deployments fetched
  * Example:
  ```json
  {
    "deployments": [
      {
        "id": 123,
        "state": "deployed",
        "version": 2,
        "active": true,
        "deployed_at": "2016-04-23T18:25:43.511Z"
      },
      {
        "id": 456,
        "state": "deployed",
        "version": 1,
        "deployed_at": "2016-04-22T18:25:43.511Z"
      },
    ]
  }
  ```

* **404** - Project not found
  * Example:
  ```json
  {
    "error": "not_found",
    "error_description": "project could not be found"
  }
  ```
