import { SnowClient } from '@elmissouri16/snow-sdk';

// Live provider use is opt-in: SNOW_PROVIDER=chatgpt after `snow login chatgpt`.
const provider = process.env.SNOW_PROVIDER ?? 'fake';
const client = await SnowClient.start({
  cwd: process.cwd(),
  executable: process.env.SNOW_EXECUTABLE ?? 'snow',
  provider,
  noSession: true,
  permission: 'deny',
});

// Start draining immediately, before submitting a prompt or awaiting its result.
const drain = (async () => {
  for await (const event of client.events()) {
    if (event.type === 'text_delta' && typeof event.text === 'string') {
      process.stdout.write(event.text);
    }
  }
})();
// Handle a transport failure immediately, even while prompt admission is pending.
const observedDrain = drain.catch((error) => {
  console.error('Event stream failed:', error);
  process.exitCode = 1;
});

try {
  const run = await client.prompt(process.env.SNOW_PROMPT ?? 'Say hello briefly.');
  const result = await run.result;
  console.log(`\nPrompt ${run.requestId}: ${result.status}`);
  if (result.status !== 'completed') process.exitCode = 1;
  if (provider === 'fake') {
    console.log('The CLI fake provider completes without text; no credentials needed.');
  }
} finally {
  await client.close();
  await observedDrain;
}
