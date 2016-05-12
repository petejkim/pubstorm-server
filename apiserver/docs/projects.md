# Projects

## Creating a New Project

```
POST /projects
```

**POST Form Params**

| Key  | Type         | Required? | Description  | Format                                  |
| ---- | ------------ | --------- | ------------ | --------------------------------------- |
| name | string[3,63] | Required  | project name | subdomain format (RFC 1034 Section 3.5) |

**Possible responses**

* **201** - Project created
  Example:
  ```json
  {
    "project": {
      "name": "atlas-react-app"
    }
  }
  ```

* **422** - Invalid params
  Example:
  ```json
  {
    "error": "invalid_params",
    "errors": {
      "name": "is required"
    }
  }
  ```

  ```json
  {
    "error": "invalid_params",
    "errors": {
      "name": "is taken"
    }
  }
  ```

## Fetching a project

```
GET /projects/:projectName
```

**Possible responses**

* **200** - Project fetched
  * Example:
  ```json
  {
    "project": {
      "name": "foo-bar-express",
      "default_domain_enabled": true
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

## Fetch list of projects

```
GET /projects
```

**Possible responses**

* **200** - Projects fetched
  * Example:
  ```json
  {
    "projects": [
      {
        "name": "foo-bar-express",
        "default_domain_enabled": true
      },
      {
        "name": "baz-cloud",
        "default_domain_enabled": false
      }
    ]
  }
  ```

## Update a project

```
PUT /projects/:projectName
```

**POST Form Params**

| Key                    | Type     | Required? | Description           |
| -----------------------| -------- | --------- | ----------------------|
| default_domain_enabled | boolean  | Optional  | enable default domain |

**Possible responses**

* **200** - Project updated
  * Example:
  ```json
  {
    "project":
    {
      "name": "foo-bar-express",
      "default_domain_enabled": true
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

## Delete a project

```
DELETE /projects/:projectName
```

**Possible responses**

* **200** - Project deleted
  * Example:
  ```json
  {
    "deleted": true
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
