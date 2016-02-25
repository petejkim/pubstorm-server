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

## Confirming user's email address

```
POST /user/confirm
```

**POST Form Params**

| Key                | Type   | Required? | Description       |
| ------------------ | ------ | --------- | ----------------- |
| email              | string | Required  | Email address     |
| confirmation\_code | string | Required  | Confirmation Code |

**Possible responses**

* **200** - Confirmed
  Example:
  ```json
  {
    "confirmed": true
  }
  ```

* **422** - Invalid params
  Example:
  ```json
  {
    "confirmed": false,
    "error": "invalid_params",
    "error_description": "invalid email or confirmation_code"
  }
  ```
