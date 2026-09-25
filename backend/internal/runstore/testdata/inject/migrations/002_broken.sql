-- Synthetic second migration with a deliberate failure in its second
-- statement. The first statement succeeds, which is the point: the runner must
-- roll the whole migration back, leaving no table and no version row behind.
CREATE TABLE inject_second (
    id TEXT PRIMARY KEY
);

THIS STATEMENT IS NOT VALID SQL;
