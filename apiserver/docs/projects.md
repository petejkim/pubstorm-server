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
