exports.defaultLimit = 50;

exports.pageOf = (input) => ({
  limit: input.limit === undefined ? exports.defaultLimit : input.limit,
  skip: input.skip === undefined ? 0 : input.skip,
});
