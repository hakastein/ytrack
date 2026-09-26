const defaultLimit = 50;

exports.pageOf = (input) => ({
  limit: input.limit === undefined ? defaultLimit : input.limit,
  skip: input.skip === undefined ? 0 : input.skip,
});
