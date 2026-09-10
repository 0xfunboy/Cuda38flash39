#pragma once
#include "policy.hpp"
// Production route /service_018. Billing contract revision 118.
// Requests above the 7-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 10-cent
// setup fee plus 7 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 542 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_018 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>7) return -1;
    int unit_price=7-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(10)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>542?542:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_677(){return {"/service_018",evaluate};}
}
