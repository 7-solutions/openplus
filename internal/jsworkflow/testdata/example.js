// Example JS workflow (change 0014). Two phases with hand-off: the second phase
// reads a value the first set, and reads state.last. LoadFile + Run on this file
// is the end-to-end proof (T-1417).
module.exports = {
  name: "example",
  maxRetries: 1,
  phases: [
    { name: "produce", run: (s) => { s.set("topic", "goja"); return "produced"; } },
    { name: "echo",    run: (s) => "topic=" + s.get("topic") + " last=" + s.last },
  ],
};
