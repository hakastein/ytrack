// The SDK merges a field named twice, so the defaults and what + adds are simply joined.
exports.fieldsOf = (given, defaults) => {
  if (given === undefined || given.trim() === "") {
    return defaults;
  }
  if (given.trim().startsWith("+")) {
    return defaults + "," + given.trim().slice(1);
  }
  return given;
};
