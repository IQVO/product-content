Feature: Content follows the catalogue through offer-availability events
  CatalogItemView is fed by CatalogItemCreated and CatalogItemDiscontinued. A
  discontinued item can neither get new content nor publish, and its content
  is retired by the policy RetireOnDiscontinue.

  Background:
    Given a category "Bottles" exists

  Scenario: Discontinuing a published item retires its content
    Given offer-availability reports item "sku-1" created
    And the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.",
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    And the API posts to "/product-contents/sku-1:publish" with Idempotency-Key "p1"
    When offer-availability reports item "sku-1" discontinued
    And the API requests "/product-contents/sku-1"
    Then the API response field "state" is "RETIRED"
    And the API response field "retiredReason" is "DISCONTINUED"
    And 1 "ProductContentRetired" events were published

  Scenario: Discontinuing an item retires its draft without an event
    Given offer-availability reports item "sku-2" created
    And the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-2","description":"Draft only."}
      """
    When offer-availability reports item "sku-2" discontinued
    And the API requests "/product-contents/sku-2"
    Then the API response field "retiredReason" is "DISCONTINUED"
    And 0 "ProductContentRetired" events were published

  Scenario: A redelivered discontinuation changes nothing
    Given offer-availability reports item "sku-1" created
    And the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.",
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    And the API posts to "/product-contents/sku-1:publish" with Idempotency-Key "p1"
    When offer-availability reports item "sku-1" discontinued
    And the discontinuation of "sku-1" is delivered again
    Then 1 "ProductContentRetired" events were published

  Scenario: Content for a discontinued item is refused
    Given offer-availability reports item "sku-3" discontinued
    When the API posts to "/product-contents" with Idempotency-Key "c3" the body:
      """
      {"catalogItemId":"sku-3","description":"Too late."}
      """
    Then the API answers 422 with problem "item-discontinued"
