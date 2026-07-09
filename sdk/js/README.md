# @gitlawb/zero-sdk

JavaScript client for `zero serve --http`.

```js
import { createZeroClient } from '@gitlawb/zero-sdk'

const zero = createZeroClient({
  baseUrl: 'http://127.0.0.1:4096',
  token: process.env.ZERO_SERVER_TOKEN,
})

const session = await zero.session.create({ title: 'SDK run' })
```
