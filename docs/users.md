# Users

## User Creation

```
POST /users
```

**POST Form Params**

| Key      | Type           | Required? | Description   |
| -------- | -------------- | --------- | ------------- |
| email    | string[5, 255] | Required  | Email address |
| password | string[6, 72]  | Required  | Password      |

**Possible responses**

* **201** - Created
  Example:
  ```json
  {
    "user": {
      "email": "foo@example.com",
      "name": "",
      "organization": ""
    }
  }
  ```

* **422** - Invalid params
  Example:
  ```json
  {
    "error": "invalid_params",
    "errors": {
      "password": "is too short (min. 6 characters)"
    }
  }
  ```
