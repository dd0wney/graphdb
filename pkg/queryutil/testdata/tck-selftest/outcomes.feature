# Fixtures for TestTCKHarnessSelfTest. Written for this repository.
# Each scenario name is a key of the expected-outcome table in tck_harness_test.go.
Feature: Harness self-test

  Scenario: passes
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
    And no side effects

  Scenario: wrong expected value
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x |
      | 2 |

  Scenario: integer against float
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x   |
      | 1.0 |

  Scenario: float against integer
    Given any graph
    When executing query:
      """
      RETURN 1.0 AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: string against integer
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x   |
      | '1' |

  Scenario: list order matters
    Given any graph
    When executing query:
      """
      RETURN [1, 2] AS x
      """
    Then the result should be, in any order:
      | x      |
      | [2, 1] |

  Scenario: rows in the wrong sequence, in order
    Given an empty graph
    And having executed:
      """
      CREATE (:A {v: 1}), (:A {v: 2})
      """
    When executing query:
      """
      MATCH (n:A)
      RETURN n.v AS x
      ORDER BY n.v
      """
    Then the result should be, in order:
      | x |
      | 2 |
      | 1 |

  Scenario: rows in the wrong sequence, any order
    Given an empty graph
    And having executed:
      """
      CREATE (:A {v: 1}), (:A {v: 2})
      """
    When executing query:
      """
      MATCH (n:A)
      RETURN n.v AS x
      ORDER BY n.v
      """
    Then the result should be, in any order:
      | x |
      | 2 |
      | 1 |

  Scenario: duplicate rows are a multiset
    Given an empty graph
    And having executed:
      """
      CREATE (:A {v: 1}), (:A {v: 2}), (:A {v: 2})
      """
    When executing query:
      """
      MATCH (n:A)
      RETURN n.v AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
      | 1 |
      | 2 |

  Scenario: wrong column name
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | y |
      | 1 |

  Scenario: query that does not parse
    Given any graph
    When executing query:
      """
      RETURN RETURN RETURN
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: setup that does not parse
    Given an empty graph
    And having executed:
      """
      CREATE (((
      """
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: error expected and the query is refused
    Given any graph
    When executing query:
      """
      RETURN RETURN RETURN
      """
    Then a SyntaxError should be raised at compile time: InvalidSyntax

  Scenario: error expected and the query runs
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then a SyntaxError should be raised at compile time: InvalidSyntax

  Scenario: no side effects after a create
    Given an empty graph
    When executing query:
      """
      CREATE (n:A {n: 1})
      RETURN n.n AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
    And no side effects

  Scenario: side effects table that holds
    Given an empty graph
    When executing query:
      """
      CREATE (n:A {n: 1})
      RETURN n.n AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
    And the side effects should be:
      | +nodes      | 1 |
      | +labels     | 1 |
      | +properties | 1 |

  Scenario: side effects table that is wrong
    Given an empty graph
    When executing query:
      """
      CREATE (n:A {n: 1})
      RETURN n.n AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
    And the side effects should be:
      | +nodes | 2 |

  Scenario: unknown step
    Given a graph with a thing that the harness does not know
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: node literal in the result
    Given an empty graph
    When executing query:
      """
      CREATE (n:A)
      RETURN n
      """
    Then the result should be, in any order:
      | n    |
      | (:A) |

  Scenario: result step that is not read
    Given any graph
    When executing query:
      """
      RETURN [1, 2] AS x
      """
    Then the result should be (ignoring element order for lists):
      | x      |
      | [2, 1] |

  Scenario: expected empty and rows come back
    Given any graph
    When executing query:
      """
      RETURN 1 AS x
      """
    Then the result should be empty

  Scenario: expected empty and none come back
    Given an empty graph
    When executing query:
      """
      MATCH (n:Nothing)
      RETURN n
      """
    Then the result should be empty

  Scenario: parameter is bound
    Given any graph
    And parameters are:
      | p | 7 |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x |
      | 7 |

  Scenario Outline: outline
    Given any graph
    When executing query:
      """
      RETURN <v> AS x
      """
    Then the result should be, in any order:
      | x   |
      | <e> |

    Examples:
      | v | e |
      | 1 | 1 |
      | 2 | 3 |

  Scenario: error expected and the query fails when it runs
    Given any graph
    When executing query:
      """
      RETURN 1 / 0 AS x
      """
    Then a ArgumentError should be raised at runtime: NumberOutOfRange
