# Findings schema

## Specialist output: `findings/<job>.json`

```json
{
  "job": "security",
  "findings": [
    {
      "file": "src/api/users.ts",
      "lines": [120, 134],
      "category": "security",
      "severity": "high",
      "title": "PATCH /users/:id has no ownership check",
      "claim": "Any authenticated user can update another user's profile.",
      "trigger": "As user A, send PATCH /users/<B's id> with a new email. The handler updates B without comparing req.user.id to the id param.",
      "evidence": "users.ts:126 loads by params.id and saves at :133 with no authz call between. Sibling handlers call requireOwner() (orders.ts:41).",
      "suggested_fix": "Call requireOwner(req, params.id) before loading the user.",
      "confidence": 0.8
    }
  ],
  "escalations": [
    {
      "file": "src/auth/session.ts",
      "lines": [40, 75],
      "reason": "token refresh logic too involved to judge confidently"
    }
  ],
  "handoffs": [
    {
      "file": "src/api/users.ts",
      "lines": [200, 210],
      "concern": "performance",
      "note": "query inside loop over all users"
    }
  ],
  "uncovered": []
}
```

- `lines`: line numbers in the code tree (head side). For PR-wide findings use `"file": ""` and `"lines": [0, 0]`.
- `category`: your specialist name.
- `trigger`: how to make it go wrong. The verifier starts here, so make it concrete.
- Keep text fields short. Empty arrays are fine.

## Verifier output: `verified/<batch>.json`

```json
{
  "batch": "senior-1",
  "results": [
    {
      "id": "security-1",
      "status": "confirmed",
      "severity": "high",
      "evidence": "Test in scratch: PATCH /users/2 as user 1 returned 200 and changed user 2's email. Test code: …",
      "notes": "",
      "duplicate_of": null
    }
  ]
}
```

- `status`: `confirmed` | `plausible` | `unproven` | `refuted` | `duplicate`. Only confirmed and plausible reach the user.
- `severity`: final severity; explain any change in `notes`.
- `duplicate_of`: id of the finding kept instead, when status is `duplicate`.
