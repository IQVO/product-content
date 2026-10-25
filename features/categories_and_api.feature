Feature: The category tree and the API conventions
  The CategoryTree is one live aggregate: every command applies to it and a
  publish announces the new version. Every POST needs an Idempotency-Key.

  Scenario: Categories nest and the tree is published as a new version
    Given a category "Drinkware" exists
    When the API posts to "/categories" with Idempotency-Key "k1" the body:
      """
      {"parentId":"{category:Drinkware}","name":"Bottles"}
      """
    Then the API answers 201
    When the API posts to "/categories:publish" with Idempotency-Key "k2"
    Then the API answers 200
    # two creates and the explicit publish each announce a new tree version
    And 3 "CategoryTreePublished" events were published

  Scenario: Sibling categories never share a name
    Given a category "Drinkware" exists
    When the API posts to "/categories" with Idempotency-Key "k3" the body:
      """
      {"name":"Drinkware"}
      """
    Then the API answers 422 with problem "duplicate-sibling-name"

  Scenario: A POST without an Idempotency-Key is refused
    When the API posts to "/categories" without an Idempotency-Key and body:
      """
      {"name":"Anything"}
      """
    Then the API answers 400 with problem "idempotency-key-required"

  Scenario: A replayed request answers from the record
    When the API posts to "/categories" with Idempotency-Key "k4" the body:
      """
      {"name":"Cups"}
      """
    And the API posts to "/categories" with Idempotency-Key "k4" the body:
      """
      {"name":"Cups"}
      """
    Then the API answers 201
    And the response is marked replayed

  Scenario: The same key with another body is refused as a reuse
    When the API posts to "/categories" with Idempotency-Key "k5" the body:
      """
      {"name":"Cups"}
      """
    And the API posts to "/categories" with Idempotency-Key "k5" the body:
      """
      {"name":"Mugs"}
      """
    Then the API answers 422 with problem "idempotency-key-reused"
