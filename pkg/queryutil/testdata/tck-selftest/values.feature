# Fixtures for TestTCKHarnessSelfTest that aim at one comparison rule each.
# Written for this repository.
Feature: Harness self-test, comparison rules

  Scenario: extra column in the output
    Given any graph
    When executing query:
      """
      RETURN 1 AS x, 2 AS y
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: columns in a different order
    Given any graph
    When executing query:
      """
      RETURN 1 AS a, 2 AS b
      """
    Then the result should be, in any order:
      | b | a |
      | 2 | 1 |

  Scenario: map equal
    Given any graph
    And parameters are:
      | p | {a: 1, b: 'q'} |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x              |
      | {b: 'q', a: 1} |

  Scenario: map with an extra key in the expected value
    Given any graph
    And parameters are:
      | p | {a: 1} |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x            |
      | {a: 1, b: 2} |

  Scenario: map with an extra key in the actual value
    Given any graph
    And parameters are:
      | p | {a: 1, b: 2} |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x      |
      | {a: 1} |

  Scenario: NaN equals NaN
    Given any graph
    And parameters are:
      | p | NaN |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x   |
      | NaN |

  Scenario: NaN against a float
    Given any graph
    And parameters are:
      | p | NaN |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x   |
      | 1.0 |

  Scenario: negative infinity equals negative infinity
    Given any graph
    And parameters are:
      | p | -Infinity |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x         |
      | -Infinity |

  Scenario: negative infinity against positive infinity
    Given any graph
    And parameters are:
      | p | -Infinity |
    When executing query:
      """
      RETURN $p AS x
      """
    Then the result should be, in any order:
      | x        |
      | Infinity |

  Scenario: extra row in the output
    Given an empty graph
    And having executed:
      """
      CREATE (:A {v: 1}), (:A {v: 2})
      """
    When executing query:
      """
      MATCH (n:A)
      RETURN n.v AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |

  Scenario: missing row in the output
    Given an empty graph
    And having executed:
      """
      CREATE (:A {v: 1}), (:A {v: 2})
      """
    When executing query:
      """
      MATCH (n:A)
      RETURN n.v AS x
      """
    Then the result should be, in any order:
      | x |
      | 1 |
      | 2 |
      | 3 |

  Scenario: string content differs
    Given any graph
    When executing query:
      """
      RETURN 'a' AS x
      """
    Then the result should be, in any order:
      | x   |
      | 'b' |

  Scenario: boolean value differs
    Given any graph
    When executing query:
      """
      RETURN true AS x
      """
    Then the result should be, in any order:
      | x     |
      | false |

  Scenario: side effects table omits a counter
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
      | +nodes | 1 |

  Scenario: pipe in a table cell
    Given any graph
    When executing query:
      """
      RETURN 'a|b' AS x
      """
    Then the result should be, in any order:
      | x      |
      | 'a\|b' |

  Scenario: newline escape in a string
    Given any graph
    When executing query:
      """
      RETURN 'a\nb' AS x
      """
    Then the result should be, in any order:
      | x       |
      | 'a\\nb' |
