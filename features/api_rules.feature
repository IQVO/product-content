# api_rules.feature — documentation-derived scenarios (bdd wave 3) for the
# conventions every write route shares (use-cases "How save and publish
# commit", the Idempotency-Key rules, troubleshooting "REST responses") and for
# the paged read product-discovery bootstraps from.
Feature: API conventions and the bootstrap read from the documentation
  A repeated key returns the stored first answer, a failed first answer
  included. The paged read lists content of every state, ordered by item id.

  Background:
    Given a category "Bottles" exists

  # --- Idempotency-Key --------------------------------------------------

  Scenario: An Idempotency-Key of 128 characters is accepted
    When the API posts to "/categories" with an Idempotency-Key of 128 characters the body:
      """
      {"name":"Cups"}
      """
    Then the API answers 201

  Scenario: An Idempotency-Key of 129 characters is malformed
    When the API posts to "/categories" with an Idempotency-Key of 129 characters the body:
      """
      {"name":"Cups"}
      """
    Then the API answers 400 with problem "idempotency-key-invalid"
    And 1 "CategoryTreePublished" events were published

  Scenario: The same key on another resource is a reuse
    Given item "sku-1" has a complete draft in category "Bottles"
    And item "sku-2" has a complete draft in category "Bottles"
    When the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "shared"
    And the API posts to "/product-contents/sku-2/publications" with Idempotency-Key "shared"
    Then the API answers 409 with problem "idempotency-key-reused"
    When the API requests "/product-contents/sku-2"
    Then the API response field "state" is "DRAFT"

  Scenario: A replayed publish raises one event and answers from the record
    Given item "sku-1" has a complete draft in category "Bottles"
    When the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "p1"
    And the API posts to "/product-contents/sku-1/publications" with Idempotency-Key "p1"
    Then the API answers 200
    And the response is marked replayed
    And 1 "ProductContentPublished" events were published

  Scenario: A replayed retirement is not the conflict a second retirement is
    Given item "sku-1" has published content in category "Bottles"
    When the API posts to "/product-contents/sku-1/retirements" with Idempotency-Key "t1"
    And the API posts to "/product-contents/sku-1/retirements" with Idempotency-Key "t1"
    Then the API answers 200
    And the response is marked replayed
    And 1 "ProductContentRetired" events were published

  Scenario: A replayed category create makes one category and one tree version
    When the API posts to "/categories" with Idempotency-Key "k1" the body:
      """
      {"name":"Cups"}
      """
    And the API posts to "/categories" with Idempotency-Key "k1" the body:
      """
      {"name":"Cups"}
      """
    Then the API answers 201
    And the response is marked replayed
    And the live category tree has the paths "Bottles, Cups"
    And 2 "CategoryTreePublished" events were published

  Scenario: A failed first answer is the answer the key keeps
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-9","description":"Late fact."}
      """
    Then the API answers 422 with problem "unknown-catalog-item"
    When offer-availability reports item "sku-9" created
    And the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-9","description":"Late fact."}
      """
    Then the API answers 422 with problem "unknown-catalog-item"
    And the response is marked replayed
    When the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-9","description":"Late fact."}
      """
    Then the API answers 201

  # --- the bootstrap read -----------------------------------------------

  Scenario: The list is ordered by item id and walked by cursor
    Given item "sku-3" has a complete draft in category "Bottles"
    And item "sku-1" has a complete draft in category "Bottles"
    And item "sku-2" has a complete draft in category "Bottles"
    When the API requests "/product-contents?limit=2"
    Then the API response path "items.0.catalogItemId" is "sku-1"
    And the API response path "items.1.catalogItemId" is "sku-2"
    And the API response list "items" has 2 items
    And the API response has a field "nextCursor"
    When the API requests the next page of "/product-contents?limit=2"
    Then the API response list "items" has 1 items
    And the API response path "items.0.catalogItemId" is "sku-3"
    And the API response has no field "nextCursor"

  Scenario: A last page that exactly fills the limit has no next cursor
    Given item "sku-1" has a complete draft in category "Bottles"
    And item "sku-2" has a complete draft in category "Bottles"
    When the API requests "/product-contents?limit=2"
    Then the API response list "items" has 2 items
    And the API response has no field "nextCursor"

  Scenario: The list holds content of every state
    Given item "sku-1" has a complete draft in category "Bottles"
    And item "sku-2" has published content in category "Bottles"
    And item "sku-3" has published content in category "Bottles"
    And the API retires "sku-3"
    When the API requests "/product-contents"
    Then the API response list "items" has 3 items
    And the API response path "items.0.state" is "DRAFT"
    And the API response path "items.1.state" is "PUBLISHED"
    And the API response path "items.2.state" is "RETIRED"

  Scenario: The page size is between 1 and 500
    When the API requests "/product-contents?limit=500"
    Then the API answers 200
    When the API requests "/product-contents?limit=501"
    Then the API answers 400 with problem "invalid-limit"
