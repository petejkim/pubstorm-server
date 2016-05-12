# Domains

## Fetching all domain names from specified project

```
GET /projects/:project_name/domains
```

**Possible responses**

* **200** - Domain names fetched
  Example:
  ```json
  {
    "domains": [
      "atlas-react-app.pubstorm.cloud",
      "www.atlas-react-app.com"
    ]
  }
  ```

* **404** - Project not found
  Example:
  ```json
  {
    "error": "not found",
    "error_message": "project could not be found"
  }
  ```


## Adding a new domain name to a project

```
POST /projects/:project_name/domains
```

**POST Form Params**

| Key  | Type          | Required? | Description  | Format                                  |
| ---- | ------------- | --------- | ------------ | --------------------------------------- |
| name | string[3,255] | Required  | domain name  | domain format (RFC 1035 Section 2.3.1)  |

**Possible responses**

* **201** - Domain created
  Example:
  ```json
  {
    "domain": {
      "name": "www.atlas-react-app.com"
    }
  }
  ```

* **404** - Project not found
  Example:
  ```json
  {
    "error": "not found",
    "error_message": "project could not be found"
  }
  ```

* **422** - Invalid params
  Example:
  ```json
  {
    "error": "invalid_params",
    "errors": {
      "name": "is invalid"
    }
  }
  ```

  ```json
  {
    "error": "invalid_request",
    "error_description": "project cannot have more domains"
  }
  ```

## Deleting a domain name from a project

```
DELETE /projects/:project_name/domains/:name
```

**Possible responses**

* **200** - Domain
  Example:
  ```json
  {
    "deleted": true
  }
  ```

* **404** - Project not found
  Example:
  ```json
  {
    "error": "not found",
    "error_message": "project could not be found"
  }
  ```

## Fetch list of domains

```
GET /projects/:projectName/domains
```

**Possible responses**

* **200** - Domains fetched
  * Example:
  ```json
  {
    "domains": [
      "foo-bar-express.pubstorm.site",
      "www.foo-bar-express.com",
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
