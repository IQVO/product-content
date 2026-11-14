# catalogue_policy_rules.feature — documentation-derived scenarios (bdd wave 3)
# for the CatalogItemView projection, the RetireOnDiscontinue policy and the
# idempotent consumption rule (domain-events "Consumed", aggregate canvas
# "Corrective Policies", eventstorming board 3).
Feature: Catalogue facts and the RetireOnDiscontinue policy from the documentation
  CatalogItemView is the only source of "the item exists" and "the item is
  discontinued". The policy retires content without a human, once per fact.

  Background:
    Given a category "Bottles" exists

  # --- the projection ---------------------------------------------------

  Scenario: Content is refused until the item's CatalogItemCreated has been consumed
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-7","description":"Early bird."}
      """
    Then the API answers 422 with problem "unknown-catalog-item"
    When offer-availability reports item "sku-7" created
    And the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-7","description":"Early bird."}
      """
    Then the API answers 201

  Scenario: A discontinued item is never revived by a later creation fact
    Given offer-availability reports item "sku-8" created
    And offer-availability reports item "sku-8" discontinued
    When offer-availability reports item "sku-8" created
    And the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-8","description":"Revived?"}
      """
    Then the API answers 422 with problem "item-discontinued"

  Scenario: A redelivered fact is applied once, judged on the CloudEvents id
    Given offer-availability delivers the creation of item "sku-1" twice
    And offer-availability delivers the discontinuation of item "sku-1" twice
    Then the inbox holds 2 events
    And the item view knows 1 items

  Scenario: Two distinct discontinuation facts for one item retire the content once
    Given item "sku-1" has published content in category "Bottles"
    When offer-availability reports item "sku-1" discontinued
    And offer-availability reports item "sku-1" discontinued
    Then 1 "ProductContentRetired" events were published
    And the inbox holds 3 events

  Scenario: An item id of 64 characters is a valid id, one of 65 is a poison message
    Given offer-availability reports an item with an id of 64 characters created
    When the API creates content for the item with an id of 64 characters
    Then the API answers 201
    When offer-availability reports a created item with an id of 65 characters which is refused as a poison message
    And offer-availability reports a discontinued item with an id of 65 characters which is refused as a poison message
    Then the item view knows 1 items

  Scenario: An item id may carry dots, underscores, colons and dashes
    Given offer-availability reports item "SKU_1.a:b-2" created
    When the API posts to "/product-contents" with a fresh Idempotency-Key the body:
      """
      {"catalogItemId":"SKU_1.a:b-2","description":"Punctuated."}
      """
    Then the API answers 201

  Scenario Outline: A fact with an invalid item id is a poison message and changes nothing (<kind>, "<id>")
    When offer-availability reports a <kind> item with the id "<id>" which is refused as a poison message
    Then the item view knows 0 items
    And the inbox holds 0 events

    Examples:
      | kind          | id                       |
      | created       | has a space              |
      | created       | slash/id                 |
      | discontinued  | has a space              |
      | discontinued  | slash/id                 |

  # --- RetireOnDiscontinue ----------------------------------------------

  Scenario: The policy retires only the discontinued item's content
    Given item "sku-1" has published content in category "Bottles"
    And item "sku-2" has published content in category "Bottles"
    When offer-availability reports item "sku-1" discontinued
    And the API requests "/product-contents/sku-1"
    Then the API response field "state" is "RETIRED"
    When the API requests "/product-contents/sku-2"
    Then the API response field "state" is "PUBLISHED"
    And 1 "ProductContentRetired" events were published

  Scenario: The retirement by the policy names the revision that was retired
    Given item "sku-1" has published content in category "Bottles"
    And the API revises "sku-1" with a complete body in category "Bottles" and description "A better bottle."
    When offer-availability reports item "sku-1" discontinued
    Then the latest "ProductContentRetired" event of "sku-1" has revision 2

  Scenario: The policy leaves content that is already retired alone
    Given item "sku-1" has published content in category "Bottles"
    And the API retires "sku-1"
    When offer-availability reports item "sku-1" discontinued
    And the API requests "/product-contents/sku-1"
    Then the API response field "state" is "RETIRED"
    And the API response field "retiredReason" is not "DISCONTINUED"
    And 1 "ProductContentRetired" events were published

  Scenario: A discarded draft keeps its reason when the item is discontinued later
    Given item "sku-1" has a complete draft in category "Bottles"
    And the API discards the draft of "sku-1"
    When offer-availability reports item "sku-1" discontinued
    And the API requests "/product-contents/sku-1"
    Then the API response field "retiredReason" is "DISCARDED"

  Scenario: A draft retired by the policy can never publish
    Given item "sku-1" has a complete draft in category "Bottles"
    When offer-availability reports item "sku-1" discontinued
    And the API publishes "sku-1"
    Then the API answers 409 with problem "content-retired"
    And 0 "ProductContentPublished" events were published

  Scenario: A draft retired by the policy can no longer be revised
    Given item "sku-1" has a complete draft in category "Bottles"
    When offer-availability reports item "sku-1" discontinued
    And the API revises "sku-1" with a complete body in category "Bottles" and description "Too late."
    Then the API answers 409 with problem "content-retired"

  Scenario: Discontinuing an item that has no content is harmless
    Given offer-availability reports item "sku-5" created
    When offer-availability reports item "sku-5" discontinued
    And the API requests "/product-contents/sku-5"
    Then the API answers 404 with problem "not-found"
    And 0 "ProductContentRetired" events were published
