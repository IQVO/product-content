Feature: Product content moves DRAFT, PUBLISHED, RETIRED
  Content enriches a catalogue item that offer-availability owns. It starts as a
  DRAFT, is published once it is complete, can be revised and published again
  with a new revision, and is retired for good.

  Background:
    Given a category "Bottles" exists
    And offer-availability reports item "sku-1" created

  Scenario: A complete draft is published and raises one event
    When the API posts to "/product-contents" with Idempotency-Key "c1" the body:
      """
      {"catalogItemId":"sku-1","description":"A steel bottle.","bullets":["Double-wall"],
       "media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],
       "primaryCategoryId":"{category:Bottles}"}
      """
    Then the API answers 201
    And the API response field "state" is "DRAFT"
    When the API posts to "/product-contents/sku-1:publish" with Idempotency-Key "p1"
    Then the API answers 200
    And the API response field "state" is "PUBLISHED"
    And 1 "ProductContentPublished" events were published

  Scenario: Publishing an incomplete draft is refused
    When the API posts to "/product-contents" with Idempotency-Key "c2" the body:
      """
      {"catalogItemId":"sku-1","description":"No picture yet."}
      """
    Then the API answers 201
    When the API posts to "/product-contents/sku-1:publish" with Idempotency-Key "p2"
    Then the API answers 422 with problem "incomplete-content"
    And 0 "ProductContentPublished" events were published

  Scenario: A draft that is discarded retires without an event
    When the API posts to "/product-contents" with Idempotency-Key "c3" the body:
      """
      {"catalogItemId":"sku-1","description":"Throw away."}
      """
    And the API posts to "/product-contents/sku-1:discard" with Idempotency-Key "d3"
    Then the API answers 200
    And the API response field "state" is "RETIRED"
    And the API response field "retiredReason" is "DISCARDED"
    And 0 "ProductContentRetired" events were published

  Scenario: Content of an unknown item is refused
    When the API posts to "/product-contents" with Idempotency-Key "c4" the body:
      """
      {"catalogItemId":"sku-unknown","description":"Who?"}
      """
    Then the API answers 422 with problem "unknown-catalog-item"
